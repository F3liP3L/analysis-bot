package binance

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/binancebot/domain/ports"
	"go.uber.org/zap"
)

// MarketDataAdapter implementa el puerto MarketDataPort para Binance
type MarketDataAdapter struct {
	client      *binance.Client
	wsClient    *binance.WebsocketClient
	logger      *zap.Logger
	
	// Caché de datos para cálculos de volatilidad
	dataMutex   sync.RWMutex
	priceCache  map[string][]float64 // Últimos precios para calcular volatilidad
	
	// Configuración
	volatilityThreshold float64
	volumeThreshold     float64
	minSpreadPercentage float64
}

// NewMarketDataAdapter crea un nuevo adaptador para datos de mercado de Binance
func NewMarketDataAdapter(
	apiKey, apiSecret string, 
	logger *zap.Logger, 
	volatilityThreshold, volumeThreshold, minSpreadPercentage float64,
) *MarketDataAdapter {
	client := binance.NewClient(apiKey, apiSecret)
	wsClient := binance.NewWebsocketClient(false)
	
	return &MarketDataAdapter{
		client:              client,
		wsClient:            wsClient,
		logger:              logger,
		priceCache:          make(map[string][]float64),
		volatilityThreshold: volatilityThreshold,
		volumeThreshold:     volumeThreshold,
		minSpreadPercentage: minSpreadPercentage,
	}
}

// SubscribeToMarketData suscribe a actualizaciones en tiempo real para un par específico
func (a *MarketDataAdapter) SubscribeToMarketData(
	ctx context.Context, 
	symbol string,
) (<-chan ports.MarketData, error) {
	resultCh := make(chan ports.MarketData, 100)
	
	wsDepthHandler := func(event *binance.WsDepthEvent) {
		// Obtener el mejor bid y ask
		if len(event.Bids) == 0 || len(event.Asks) == 0 {
			return
		}
		
		// Calcular bid y ask
		bidPrice, err := parsePrice(event.Bids[0].Price)
		if err != nil {
			a.logger.Error("Error al parsear precio bid", zap.Error(err))
			return
		}
		
		askPrice, err := parsePrice(event.Asks[0].Price)
		if err != nil {
			a.logger.Error("Error al parsear precio ask", zap.Error(err))
			return
		}
		
		// Calcular spread
		spread := askPrice - bidPrice
		spreadPct := spread / bidPrice * 100
		
		// Calcular precio promedio
		price := (bidPrice + askPrice) / 2
		
		// Actualizar caché de precios para volatilidad
		a.updatePriceCache(symbol, price)
		
		// Calcular volatilidad
		volatility := a.calculateVolatility(symbol)
		
		// Crear y enviar datos de mercado
		marketData := ports.MarketData{
			Symbol:     symbol,
			Price:      price,
			BidPrice:   bidPrice,
			AskPrice:   askPrice,
			Spread:     spreadPct,
			Volatility: volatility,
			Timestamp:  time.Now(),
		}
		
		select {
		case resultCh <- marketData:
		default:
			// Si el canal está lleno, omitimos estos datos
		}
	}
	
	// Suscribirse al canal de profundidad (orderbook)
	_, _, err := a.wsClient.WsDepthServe(symbol, wsDepthHandler, func(err error) {
		a.logger.Error("Error en WebSocket de profundidad", 
			zap.String("symbol", symbol), zap.Error(err))
	})
	if err != nil {
		return nil, fmt.Errorf("error al suscribirse a WebSocket de profundidad: %w", err)
	}
	
	// También suscribirse al canal de trades para volumen
	wsTradeHandler := func(event *binance.WsTradeEvent) {
		price, err := parsePrice(event.Price)
		if err != nil {
			a.logger.Error("Error al parsear precio de trade", zap.Error(err))
			return
		}
		
		quantity, err := parseQuantity(event.Quantity)
		if err != nil {
			a.logger.Error("Error al parsear cantidad de trade", zap.Error(err))
			return
		}
		
		// Obtener el último dato de mercado si está disponible
		a.dataMutex.RLock()
		prices, exists := a.priceCache[symbol]
		a.dataMutex.RUnlock()
		
		if !exists || len(prices) == 0 {
			return
		}
		
		// Actualizar caché de precios para volatilidad
		a.updatePriceCache(symbol, price)
		
		// Calcular volatilidad
		volatility := a.calculateVolatility(symbol)
		
		// Crear y enviar datos de mercado actualizados
		marketData := ports.MarketData{
			Symbol:     symbol,
			Price:      price,
			Volume:     quantity,
			Volatility: volatility,
			Timestamp:  time.Now(),
		}
		
		select {
		case resultCh <- marketData:
		default:
			// Si el canal está lleno, omitimos estos datos
		}
	}
	
	_, _, err = a.wsClient.WsTradeServe(symbol, wsTradeHandler, func(err error) {
		a.logger.Error("Error en WebSocket de trades", 
			zap.String("symbol", symbol), zap.Error(err))
	})
	if err != nil {
		return nil, fmt.Errorf("error al suscribirse a WebSocket de trades: %w", err)
	}
	
	// Cerrar los WebSockets cuando el contexto sea cancelado
	go func() {
		<-ctx.Done()
		close(resultCh)
	}()
	
	return resultCh, nil
}

// GetAllSymbols obtiene todos los símbolos disponibles para trading
func (a *MarketDataAdapter) GetAllSymbols(ctx context.Context) ([]string, error) {
	info, err := a.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("error al obtener información de exchange: %w", err)
	}
	
	symbols := make([]string, 0, len(info.Symbols))
	for _, s := range info.Symbols {
		// Solo incluir símbolos que estén activos para trading
		if s.Status == "TRADING" {
			symbols = append(symbols, s.Symbol)
		}
	}
	
	return symbols, nil
}

// GetMarketData obtiene datos actuales del mercado para un símbolo específico
func (a *MarketDataAdapter) GetMarketData(ctx context.Context, symbol string) (ports.MarketData, error) {
	// Obtener libro de órdenes para bid/ask
	depth, err := a.client.NewDepthService().Symbol(symbol).Limit(5).Do(ctx)
	if err != nil {
		return ports.MarketData{}, fmt.Errorf("error al obtener profundidad: %w", err)
	}
	
	if len(depth.Bids) == 0 || len(depth.Asks) == 0 {
		return ports.MarketData{}, fmt.Errorf("no hay bids o asks disponibles para %s", symbol)
	}
	
	// Obtener datos de 24h para volumen y otros datos
	ticker24h, err := a.client.NewTicker24hService().Symbol(symbol).Do(ctx)
	if err != nil {
		return ports.MarketData{}, fmt.Errorf("error al obtener ticker 24h: %w", err)
	}
	
	// Parsear precios y volumen
	bidPrice, err := parsePrice(depth.Bids[0].Price)
	if err != nil {
		return ports.MarketData{}, fmt.Errorf("error al parsear precio bid: %w", err)
	}
	
	askPrice, err := parsePrice(depth.Asks[0].Price)
	if err != nil {
		return ports.MarketData{}, fmt.Errorf("error al parsear precio ask: %w", err)
	}
	
	volume, err := parseQuantity(ticker24h.Volume)
	if err != nil {
		return ports.MarketData{}, fmt.Errorf("error al parsear volumen: %w", err)
	}
	
	// Calcular precio y spread
	price := (bidPrice + askPrice) / 2
	spread := askPrice - bidPrice
	spreadPct := spread / bidPrice * 100
	
	// Actualizar caché de precios para volatilidad
	a.updatePriceCache(symbol, price)
	
	// Calcular volatilidad
	volatility := a.calculateVolatility(symbol)
	
	return ports.MarketData{
		Symbol:     symbol,
		Price:      price,
		Volume:     volume,
		BidPrice:   bidPrice,
		AskPrice:   askPrice,
		Spread:     spreadPct,
		Volatility: volatility,
		Timestamp:  time.Now(),
	}, nil
}

// AnalyzeOpportunities analiza los datos de mercado para identificar oportunidades
func (a *MarketDataAdapter) AnalyzeOpportunities(
	ctx context.Context, 
	data []ports.MarketData,
) ([]ports.Opportunity, error) {
	opportunities := make([]ports.Opportunity, 0)
	
	for _, marketData := range data {
		// Filtrar por volatilidad mínima
		if marketData.Volatility < a.volatilityThreshold {
			continue
		}
		
		// Filtrar por volumen mínimo
		if marketData.Volume < a.volumeThreshold {
			continue
		}
		
		// Filtrar por spread mínimo
		if marketData.Spread < a.minSpreadPercentage {
			continue
		}
		
		// Calcular precios de entrada, take-profit y stop-loss
		entryPrice := marketData.Price
		
		// Para una estrategia de compra (se puede ampliar para estrategias de venta)
		targetPrice := entryPrice * (1 + marketData.Spread/100) // Take profit basado en el spread
		stopLossPrice := entryPrice * (1 - marketData.Spread/200) // Stop loss a la mitad del spread
		
		// Calcular beneficio potencial y riesgo
		potentialProfit := (targetPrice - entryPrice) / entryPrice * 100
		risk := (entryPrice - stopLossPrice) / entryPrice * 100
		
		// Calcular puntuación basada en la relación riesgo/beneficio y volatilidad
		score := (potentialProfit / risk) * marketData.Volatility
		
		// Crear oportunidad
		opportunity := ports.Opportunity{
			Symbol:          marketData.Symbol,
			EntryPrice:      entryPrice,
			TargetPrice:     targetPrice,
			StopLossPrice:   stopLossPrice,
			PotentialProfit: potentialProfit,
			Risk:            risk,
			Score:           score,
			Timestamp:       time.Now(),
		}
		
		opportunities = append(opportunities, opportunity)
	}
	
	// Ordenar oportunidades por puntuación (de mayor a menor)
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].Score > opportunities[j].Score
	})
	
	return opportunities, nil
}

// updatePriceCache actualiza la caché de precios para un símbolo
func (a *MarketDataAdapter) updatePriceCache(symbol string, price float64) {
	a.dataMutex.Lock()
	defer a.dataMutex.Unlock()
	
	// Inicializar la caché para este símbolo si no existe
	if _, ok := a.priceCache[symbol]; !ok {
		a.priceCache[symbol] = make([]float64, 0, 100)
	}
	
	// Agregar el nuevo precio
	a.priceCache[symbol] = append(a.priceCache[symbol], price)
	
	// Mantener solo los últimos 100 precios
	if len(a.priceCache[symbol]) > 100 {
		a.priceCache[symbol] = a.priceCache[symbol][len(a.priceCache[symbol])-100:]
	}
}

// calculateVolatility calcula la volatilidad para un símbolo basado en los precios almacenados
func (a *MarketDataAdapter) calculateVolatility(symbol string) float64 {
	a.dataMutex.RLock()
	defer a.dataMutex.RUnlock()
	
	prices, ok := a.priceCache[symbol]
	if !ok || len(prices) < 2 {
		return 0
	}
	
	// Calcular desviación estándar (simplificado)
	var sum, sumSquared float64
	for _, price := range prices {
		sum += price
		sumSquared += price * price
	}
	
	mean := sum / float64(len(prices))
	variance := sumSquared/float64(len(prices)) - mean*mean
	
	if variance < 0 {
		// Protección contra errores de precisión que pueden resultar en varianza negativa
		variance = 0
	}
	
	stdDev := math.Sqrt(variance)
	
	// Convertir a coeficiente de variación (CV) para normalizar
	volatility := (stdDev / mean) * 100
	
	return volatility
}

// Funciones de utilidad para parsear strings a números
func parsePrice(price string) (float64, error) {
	var value float64
	_, err := fmt.Sscanf(price, "%f", &value)
	return value, err
}

func parseQuantity(quantity string) (float64, error) {
	var value float64
	_, err := fmt.Sscanf(quantity, "%f", &value)
	return value, err
} 