package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/binancebot/domain/ports"
	"go.uber.org/zap"
)

// TradingService implementa la lógica principal del bot de trading
type TradingService struct {
	marketDataPort     ports.MarketDataPort
	orderExecutionPort ports.OrderExecutionPort
	riskManagementPort ports.RiskManagementPort
	learningPort       ports.LearningPort
	logger             *zap.Logger
	
	activeTradesMutex  sync.Mutex
	activeTrades       map[string]*ActiveTrade
	
	// Configuración y estado
	targetDailyTrades  int
	minProfitUSD       float64
	dailyStats         DailyStats
}

// ActiveTrade representa un trade activo
type ActiveTrade struct {
	Symbol          string
	OrderID         int64
	EntryPrice      float64
	Quantity        float64
	Side            ports.OrderSide
	StopLossPrice   float64
	TakeProfitPrice float64
	EntryTime       time.Time
}

// DailyStats representa las estadísticas diarias
type DailyStats struct {
	Date             time.Time
	CompletedTrades  int
	SuccessfulTrades int
	TotalProfit      float64
	StartingBalance  float64
	CurrentBalance   float64
}

// NewTradingService crea una nueva instancia del servicio de trading
func NewTradingService(
	marketDataPort ports.MarketDataPort,
	orderExecutionPort ports.OrderExecutionPort,
	riskManagementPort ports.RiskManagementPort,
	learningPort ports.LearningPort,
	logger *zap.Logger,
	targetDailyTrades int,
	minProfitUSD float64,
) *TradingService {
	return &TradingService{
		marketDataPort:     marketDataPort,
		orderExecutionPort: orderExecutionPort,
		riskManagementPort: riskManagementPort,
		learningPort:       learningPort,
		logger:             logger,
		targetDailyTrades:  targetDailyTrades,
		minProfitUSD:       minProfitUSD,
		activeTrades:       make(map[string]*ActiveTrade),
		dailyStats: DailyStats{
			Date: time.Now().Truncate(24 * time.Hour),
		},
	}
}

// Start inicia el bot de trading
func (s *TradingService) Start(ctx context.Context) error {
	s.logger.Info("Iniciando bot de trading")
	
	// Obtener información de la cuenta para actualizar el saldo inicial
	account, err := s.orderExecutionPort.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("error al obtener información de la cuenta: %w", err)
	}
	
	s.dailyStats.StartingBalance = account.TotalBalance
	s.dailyStats.CurrentBalance = account.TotalBalance
	
	s.logger.Info("Saldo inicial", zap.Float64("balance", account.TotalBalance))
	
	// Obtener todos los símbolos disponibles
	symbols, err := s.marketDataPort.GetAllSymbols(ctx)
	if err != nil {
		return fmt.Errorf("error al obtener símbolos: %w", err)
	}
	
	s.logger.Info("Símbolos disponibles", zap.Int("count", len(symbols)))
	
	// Iniciar monitoreo de oportunidades en goroutines separadas
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	
	// Canal para recibir datos de mercado de todos los símbolos
	marketDataCh := make(chan []ports.MarketData, 10)
	
	// Recopilar datos de mercado de múltiples símbolos
	wg.Add(1)
	go func() {
		defer wg.Done()
		symbolData := make([]ports.MarketData, 0, len(symbols))
		
		// Crear un ticker para analizar oportunidades regularmente
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				symbolData = symbolData[:0] // Limpiar slice sin realocar
				
				// Obtener datos actuales para cada símbolo
				for _, symbol := range symbols {
					data, err := s.marketDataPort.GetMarketData(ctx, symbol)
					if err != nil {
						s.logger.Error("Error al obtener datos de mercado", 
							zap.String("symbol", symbol), zap.Error(err))
						continue
					}
					symbolData = append(symbolData, data)
				}
				
				if len(symbolData) > 0 {
					// Enviar los datos recopilados para análisis
					select {
					case marketDataCh <- symbolData:
					default:
						// Si el canal está lleno, omitimos este conjunto de datos
						s.logger.Warn("Canal de datos de mercado lleno, omitiendo datos")
					}
				}
			}
		}
	}()
	
	// Analizar oportunidades y ejecutar trades
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case marketData := <-marketDataCh:
				// Analizar oportunidades de trading
				opportunities, err := s.marketDataPort.AnalyzeOpportunities(ctx, marketData)
				if err != nil {
					s.logger.Error("Error al analizar oportunidades", zap.Error(err))
					continue
				}
				
				// Procesar las oportunidades encontradas
				if len(opportunities) > 0 {
					s.processOpportunities(ctx, opportunities)
				}
			}
		}
	}()
	
	// Monitorear trades activos
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.monitorActiveTrades(ctx)
			}
		}
	}()
	
	// Actualizar el modelo de aprendizaje periódicamente
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.learningPort.UpdateModel(ctx); err != nil {
					s.logger.Error("Error al actualizar modelo de aprendizaje", zap.Error(err))
				} else {
					s.logger.Info("Modelo de aprendizaje actualizado correctamente")
				}
			}
		}
	}()
	
	// Actualizar estadísticas diarias y reiniciarlas al cambiar de día
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				today := time.Now().Truncate(24 * time.Hour)
				if !today.Equal(s.dailyStats.Date) {
					// Nuevo día, reiniciar estadísticas
					account, err := s.orderExecutionPort.GetAccount(ctx)
					if err != nil {
						s.logger.Error("Error al obtener información de la cuenta para estadísticas", zap.Error(err))
						continue
					}
					
					s.logger.Info("Resumen del día anterior",
						zap.Time("date", s.dailyStats.Date),
						zap.Int("completed_trades", s.dailyStats.CompletedTrades),
						zap.Int("successful_trades", s.dailyStats.SuccessfulTrades),
						zap.Float64("total_profit", s.dailyStats.TotalProfit),
						zap.Float64("starting_balance", s.dailyStats.StartingBalance),
						zap.Float64("ending_balance", account.TotalBalance))
					
					// Reiniciar estadísticas para el nuevo día
					s.dailyStats = DailyStats{
						Date:            today,
						StartingBalance: account.TotalBalance,
						CurrentBalance:  account.TotalBalance,
					}
				}
			}
		}
	}()
	
	// Esperar a que cualquier goroutine termine con error o el contexto sea cancelado
	go func() {
		wg.Wait()
		close(errCh)
	}()
	
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("error en la ejecución del bot: %w", err)
		}
	case <-ctx.Done():
		s.logger.Info("Deteniendo bot de trading debido a señal de contexto")
	}
	
	return nil
}

// processOpportunities procesa las oportunidades identificadas y ejecuta trades según corresponda
func (s *TradingService) processOpportunities(ctx context.Context, opportunities []ports.Opportunity) {
	// Ordenar oportunidades por score (puntuación)
	// En una implementación completa, aquí habría ordenamiento

	s.activeTradesMutex.Lock()
	activeTradesCount := len(s.activeTrades)
	s.activeTradesMutex.Unlock()
	
	// Obtener parámetros de riesgo
	riskParams, err := s.riskManagementPort.GetRiskParameters(ctx)
	if err != nil {
		s.logger.Error("Error al obtener parámetros de riesgo", zap.Error(err))
		return
	}
	
	// Verificar si podemos abrir más trades
	if activeTradesCount >= riskParams.MaxSimultaneousTrades {
		return
	}
	
	// Verificar si ya cumplimos la meta diaria
	if s.dailyStats.CompletedTrades >= s.targetDailyTrades {
		s.logger.Info("Meta diaria de trades cumplida, esperando al siguiente día")
		return
	}
	
	// Obtener información de la cuenta para calcular el capital disponible
	account, err := s.orderExecutionPort.GetAccount(ctx)
	if err != nil {
		s.logger.Error("Error al obtener información de la cuenta", zap.Error(err))
		return
	}
	
	// Procesar cada oportunidad
	for _, opportunity := range opportunities {
		// Verificar si ya tenemos un trade activo para este símbolo
		s.activeTradesMutex.Lock()
		_, exists := s.activeTrades[opportunity.Symbol]
		s.activeTradesMutex.Unlock()
		
		if exists {
			continue
		}
		
		// Validar la oportunidad según los parámetros de riesgo
		isValid, reason, err := s.riskManagementPort.ValidateTrade(ctx, opportunity, account.AvailableBalance)
		if err != nil {
			s.logger.Error("Error al validar trade", 
				zap.String("symbol", opportunity.Symbol), zap.Error(err))
			continue
		}
		
		if !isValid {
			s.logger.Debug("Oportunidad rechazada", 
				zap.String("symbol", opportunity.Symbol), zap.String("reason", reason))
			continue
		}
		
		// Predecir el éxito del trade usando el modelo de aprendizaje
		features := s.prepareModelFeatures(ctx, opportunity)
		successProbability, err := s.learningPort.PredictTradeSuccess(ctx, features)
		if err != nil {
			s.logger.Error("Error al predecir éxito del trade", 
				zap.String("symbol", opportunity.Symbol), zap.Error(err))
			continue
		}
		
		// Establecer un umbral mínimo de probabilidad de éxito
		if successProbability < 0.6 {
			s.logger.Debug("Probabilidad de éxito demasiado baja", 
				zap.String("symbol", opportunity.Symbol), 
				zap.Float64("probability", successProbability))
			continue
		}
		
		// Calcular el tamaño de la posición
		positionSize, err := s.riskManagementPort.CalculatePositionSize(
			ctx, opportunity.Symbol, account.AvailableBalance)
		if err != nil {
			s.logger.Error("Error al calcular tamaño de posición", 
				zap.String("symbol", opportunity.Symbol), zap.Error(err))
			continue
		}
		
		// Ejecutar la orden
		side := ports.OrderSideBuy // Por simplicidad, asumimos compra
		
		order, err := s.orderExecutionPort.CreateOrder(
			ctx, 
			opportunity.Symbol, 
			side,
			ports.OrderTypeLimit,
			positionSize,
			opportunity.EntryPrice,
			0, // Sin stop price en la orden inicial
		)
		if err != nil {
			s.logger.Error("Error al crear orden", 
				zap.String("symbol", opportunity.Symbol), zap.Error(err))
			continue
		}
		
		s.logger.Info("Orden creada", 
			zap.String("symbol", order.Symbol),
			zap.Int64("order_id", order.OrderID),
			zap.Float64("price", order.Price),
			zap.Float64("quantity", order.OriginalQuantity))
		
		// Registrar el trade activo
		s.activeTradesMutex.Lock()
		s.activeTrades[opportunity.Symbol] = &ActiveTrade{
			Symbol:          order.Symbol,
			OrderID:         order.OrderID,
			EntryPrice:      order.Price,
			Quantity:        order.OriginalQuantity,
			Side:            order.Side,
			StopLossPrice:   opportunity.StopLossPrice,
			TakeProfitPrice: opportunity.TargetPrice,
			EntryTime:       time.Now(),
		}
		s.activeTradesMutex.Unlock()
		
		// Solo ejecutar un trade a la vez para evitar problemas de concurrencia
		break
	}
}

// monitorActiveTrades monitorea los trades activos y gestiona stop-loss y take-profit
func (s *TradingService) monitorActiveTrades(ctx context.Context) {
	s.activeTradesMutex.Lock()
	defer s.activeTradesMutex.Unlock()
	
	for symbol, trade := range s.activeTrades {
		// Obtener datos actuales del mercado
		marketData, err := s.marketDataPort.GetMarketData(ctx, symbol)
		if err != nil {
			s.logger.Error("Error al obtener datos de mercado para monitoreo", 
				zap.String("symbol", symbol), zap.Error(err))
			continue
		}
		
		// Verificar si la orden de entrada está completa
		order, err := s.orderExecutionPort.GetOrder(ctx, symbol, trade.OrderID)
		if err != nil {
			s.logger.Error("Error al obtener orden para monitoreo", 
				zap.String("symbol", symbol), zap.Int64("order_id", trade.OrderID), zap.Error(err))
			continue
		}
		
		// Si la orden sigue pendiente y ha pasado demasiado tiempo, cancelarla
		if order.Status != ports.OrderStatusFilled {
			if time.Since(trade.EntryTime) > 5*time.Minute {
				s.logger.Info("Cancelando orden que no se ejecutó a tiempo", 
					zap.String("symbol", symbol), zap.Int64("order_id", trade.OrderID))
				
				err := s.orderExecutionPort.CancelOrder(ctx, symbol, trade.OrderID)
				if err != nil {
					s.logger.Error("Error al cancelar orden", 
						zap.String("symbol", symbol), zap.Int64("order_id", trade.OrderID), zap.Error(err))
				} else {
					delete(s.activeTrades, symbol)
				}
			}
			continue
		}
		
		// La orden está completada, verificar condiciones de salida
		currentPrice := marketData.Price
		
		// Para una posición de compra
		if trade.Side == ports.OrderSideBuy {
			// Verificar stop-loss
			if currentPrice <= trade.StopLossPrice {
				s.logger.Info("Activando stop-loss", 
					zap.String("symbol", symbol),
					zap.Float64("entry_price", trade.EntryPrice),
					zap.Float64("current_price", currentPrice),
					zap.Float64("stop_loss", trade.StopLossPrice))
				
				s.executeExitOrder(ctx, trade, currentPrice, "stop-loss")
				delete(s.activeTrades, symbol)
				continue
			}
			
			// Verificar take-profit
			if currentPrice >= trade.TakeProfitPrice {
				s.logger.Info("Activando take-profit", 
					zap.String("symbol", symbol),
					zap.Float64("entry_price", trade.EntryPrice),
					zap.Float64("current_price", currentPrice),
					zap.Float64("take_profit", trade.TakeProfitPrice))
				
				s.executeExitOrder(ctx, trade, currentPrice, "take-profit")
				delete(s.activeTrades, symbol)
				continue
			}
		}
		// Para una posición de venta (en caso de implementarse)
		// else if trade.Side == ports.OrderSideSell { ... }
	}
}

// executeExitOrder ejecuta una orden de salida para un trade
func (s *TradingService) executeExitOrder(
	ctx context.Context, 
	trade *ActiveTrade, 
	exitPrice float64,
	reason string,
) {
	// Determinar el lado opuesto para la orden de salida
	exitSide := ports.OrderSideSell
	if trade.Side == ports.OrderSideSell {
		exitSide = ports.OrderSideBuy
	}
	
	// Crear orden de salida
	order, err := s.orderExecutionPort.CreateOrder(
		ctx,
		trade.Symbol,
		exitSide,
		ports.OrderTypeMarket, // Usar orden de mercado para salida inmediata
		trade.Quantity,
		exitPrice,
		0,
	)
	if err != nil {
		s.logger.Error("Error al crear orden de salida", 
			zap.String("symbol", trade.Symbol), zap.Error(err))
		return
	}
	
	s.logger.Info("Orden de salida creada", 
		zap.String("symbol", order.Symbol),
		zap.Int64("order_id", order.OrderID),
		zap.String("reason", reason))
	
	// Calcular resultados del trade
	var profitLoss, profitLossPct float64
	if trade.Side == ports.OrderSideBuy {
		profitLoss = (exitPrice - trade.EntryPrice) * trade.Quantity
		profitLossPct = (exitPrice - trade.EntryPrice) / trade.EntryPrice * 100
	} else {
		profitLoss = (trade.EntryPrice - exitPrice) * trade.Quantity
		profitLossPct = (trade.EntryPrice - exitPrice) / trade.EntryPrice * 100
	}
	
	// Registrar resultado para aprendizaje
	result := ports.TradeResult{
		Symbol:        trade.Symbol,
		EntryPrice:    trade.EntryPrice,
		ExitPrice:     exitPrice,
		Quantity:      trade.Quantity,
		Side:          trade.Side,
		EntryTime:     trade.EntryTime,
		ExitTime:      time.Now(),
		ProfitLoss:    profitLoss,
		ProfitLossPct: profitLossPct,
		IsSuccessful:  profitLoss > 0,
		Reason:        reason,
	}
	
	if err := s.learningPort.RegisterTradeResult(ctx, result); err != nil {
		s.logger.Error("Error al registrar resultado para aprendizaje", 
			zap.String("symbol", trade.Symbol), zap.Error(err))
	}
	
	// Actualizar estadísticas diarias
	s.dailyStats.CompletedTrades++
	if profitLoss > 0 {
		s.dailyStats.SuccessfulTrades++
	}
	s.dailyStats.TotalProfit += profitLoss
	s.dailyStats.CurrentBalance += profitLoss
	
	s.logger.Info("Trade completado", 
		zap.String("symbol", trade.Symbol),
		zap.Float64("profit_loss", profitLoss),
		zap.Float64("profit_loss_pct", profitLossPct),
		zap.Bool("is_successful", profitLoss > 0))
}

// prepareModelFeatures prepara las características para el modelo de aprendizaje
func (s *TradingService) prepareModelFeatures(
	ctx context.Context, 
	opportunity ports.Opportunity,
) ports.ModelFeatures {
	// Obtener la confianza del modelo para este símbolo
	symbolConfidence, err := s.learningPort.GetSymbolConfidence(ctx, opportunity.Symbol)
	if err != nil {
		s.logger.Error("Error al obtener confianza del símbolo", 
			zap.String("symbol", opportunity.Symbol), zap.Error(err))
	}
	
	now := time.Now()
	
	return ports.ModelFeatures{
		Symbol:        opportunity.Symbol,
		Price:         opportunity.EntryPrice,
		Volume:        0, // En una implementación real, obtendríamos esto de los datos de mercado
		Volatility:    0, // En una implementación real, calcularíamos la volatilidad
		Spread:        0, // En una implementación real, obtendríamos esto de los datos de mercado
		MarketTrend:   0, // En una implementación real, calcularíamos la tendencia
		TimeOfDay:     now.Format("15:04"),
		DayOfWeek:     now.Weekday().String(),
		RecentSuccess: symbolConfidence.SuccessRate,
	}
} 