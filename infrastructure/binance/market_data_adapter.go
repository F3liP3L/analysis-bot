package binance

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/binancebot/domain/ports"
	"go.uber.org/zap"
)

// MarketDataAdapter implementa el puerto MarketDataPort para Binance
type MarketDataAdapter struct {
	client *binance.Client
	logger *zap.Logger

	// Caché de datos para cálculos de volatilidad
	dataMutex  sync.RWMutex
	priceCache map[string][]float64 // Últimos precios para calcular volatilidad

	// Configuración
	volatilityThreshold float64
	volumeThreshold     float64
	minSpreadPercentage float64

	// Canales para WebSocket
	wsStopC chan struct{}
	wsDoneC chan struct{}
}

// NewMarketDataAdapter crea un nuevo adaptador para datos de mercado de Binance
func NewMarketDataAdapter(
	apiKey, apiSecret string,
	logger *zap.Logger,
	volatilityThreshold, volumeThreshold, minSpreadPercentage float64,
) *MarketDataAdapter {
	client := binance.NewClient(apiKey, apiSecret)

	return &MarketDataAdapter{
		client:              client,
		logger:              logger,
		priceCache:          make(map[string][]float64),
		volatilityThreshold: volatilityThreshold,
		volumeThreshold:     volumeThreshold,
		minSpreadPercentage: minSpreadPercentage,
		wsStopC:             make(chan struct{}),
		wsDoneC:             make(chan struct{}),
	}
}

// StartWebSocket inicia la conexión WebSocket para un símbolo
func (a *MarketDataAdapter) StartWebSocket(symbol string) error {
	wsHandler := func(event *binance.WsMarketStatEvent) {
		a.handleMarketStatEvent(event)
	}

	errHandler := func(err error) {
		a.logger.Error("WebSocket error", zap.Error(err))
	}

	var err error
	a.wsDoneC, a.wsStopC, err = binance.WsMarketStatServe(symbol, wsHandler, errHandler)
	if err != nil {
		return fmt.Errorf("error iniciando WebSocket: %w", err)
	}

	return nil
}

// StopWebSocket detiene la conexión WebSocket
func (a *MarketDataAdapter) StopWebSocket() {
	if a.wsStopC != nil {
		close(a.wsStopC)
	}
}

// handleMarketStatEvent maneja los eventos de estadísticas del mercado
func (a *MarketDataAdapter) handleMarketStatEvent(event *binance.WsMarketStatEvent) {
	price, err := strconv.ParseFloat(event.LastPrice, 64)
	if err != nil {
		a.logger.Error("Error parsing price", zap.Error(err))
		return
	}

	a.dataMutex.Lock()
	defer a.dataMutex.Unlock()

	// Actualizar caché de precios
	if _, exists := a.priceCache[event.Symbol]; !exists {
		a.priceCache[event.Symbol] = make([]float64, 0, 100)
	}
	a.priceCache[event.Symbol] = append(a.priceCache[event.Symbol], price)

	// Mantener solo los últimos 100 precios
	if len(a.priceCache[event.Symbol]) > 100 {
		a.priceCache[event.Symbol] = a.priceCache[event.Symbol][1:]
	}
}

// Get24hTicker obtiene las estadísticas de 24 horas para un símbolo
func (a *MarketDataAdapter) Get24hTicker(ctx context.Context, symbol string) (*ports.Ticker24h, error) {
	ticker24h, err := a.client.NewListPriceChangeStatsService().Symbol(symbol).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("error obteniendo ticker 24h: %w", err)
	}

	if len(ticker24h) == 0 {
		return nil, fmt.Errorf("no se encontraron datos para el símbolo %s", symbol)
	}

	ticker := ticker24h[0]
	return &ports.Ticker24h{
		Symbol:             ticker.Symbol,
		PriceChange:        ticker.PriceChange,
		PriceChangePercent: ticker.PriceChangePercent,
		WeightedAvgPrice:   ticker.WeightedAvgPrice,
		PrevClosePrice:     ticker.PrevClosePrice,
		LastPrice:          ticker.LastPrice,
		BidPrice:           ticker.BidPrice,
		AskPrice:           ticker.AskPrice,
		OpenPrice:          ticker.OpenPrice,
		HighPrice:          ticker.HighPrice,
		LowPrice:           ticker.LowPrice,
		Volume:             ticker.Volume,
		QuoteVolume:        ticker.QuoteVolume,
		OpenTime:           ticker.OpenTime,
		CloseTime:          ticker.CloseTime,
		Count:              ticker.Count,
	}, nil
}

// SubscribeToMarketData implementa la suscripción a datos de mercado en tiempo real
func (a *MarketDataAdapter) SubscribeToMarketData(ctx context.Context, symbol string) (<-chan ports.MarketData, error) {
	marketDataChan := make(chan ports.MarketData)

	// Iniciar WebSocket
	if err := a.StartWebSocket(symbol); err != nil {
		close(marketDataChan)
		return nil, err
	}

	// Goroutine para cerrar el WebSocket cuando se cancele el contexto
	go func() {
		<-ctx.Done()
		a.StopWebSocket()
		close(marketDataChan)
	}()

	return marketDataChan, nil
}

// GetAllSymbols obtiene todos los símbolos disponibles para trading
func (a *MarketDataAdapter) GetAllSymbols(ctx context.Context) ([]string, error) {
	exchangeInfo, err := a.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("error obteniendo información de exchange: %w", err)
	}

	symbols := make([]string, len(exchangeInfo.Symbols))
	for i, symbolInfo := range exchangeInfo.Symbols {
		symbols[i] = symbolInfo.Symbol
	}

	return symbols, nil
}

// GetMarketData obtiene datos actuales del mercado para un símbolo específico
func (a *MarketDataAdapter) GetMarketData(ctx context.Context, symbol string) (ports.MarketData, error) {
	ticker24h, err := a.Get24hTicker(ctx, symbol)
	if err != nil {
		return ports.MarketData{}, err
	}

	lastPrice, _ := strconv.ParseFloat(ticker24h.LastPrice, 64)
	volume, _ := strconv.ParseFloat(ticker24h.Volume, 64)
	bidPrice, _ := strconv.ParseFloat(ticker24h.BidPrice, 64)
	askPrice, _ := strconv.ParseFloat(ticker24h.AskPrice, 64)
	spread := askPrice - bidPrice

	return ports.MarketData{
		Symbol:    symbol,
		Price:     lastPrice,
		Volume:    volume,
		BidPrice:  bidPrice,
		AskPrice:  askPrice,
		Spread:    spread,
		Timestamp: time.Now(),
	}, nil
}

// AnalyzeOpportunities analiza los datos de mercado para identificar oportunidades
func (a *MarketDataAdapter) AnalyzeOpportunities(ctx context.Context, data []ports.MarketData) ([]ports.Opportunity, error) {
	opportunities := make([]ports.Opportunity, 0)

	for _, marketData := range data {
		// Calcular volatilidad usando los datos en caché
		volatility := a.calculateVolatility(marketData.Symbol)

		// Verificar si cumple con los criterios
		if volatility >= a.volatilityThreshold &&
			marketData.Volume >= a.volumeThreshold &&
			(marketData.Spread/marketData.Price)*100 >= a.minSpreadPercentage {

			// Crear oportunidad
			opportunity := ports.Opportunity{
				Symbol:          marketData.Symbol,
				EntryPrice:      marketData.Price,
				TargetPrice:     marketData.Price * 1.02, // Target 2% arriba
				StopLossPrice:   marketData.Price * 0.99, // Stop loss 1% abajo
				PotentialProfit: marketData.Price * 0.02, // 2% potencial
				Risk:            marketData.Price * 0.01, // 1% riesgo
				Score:           calculateScore(volatility, marketData.Volume, marketData.Spread/marketData.Price),
				Timestamp:       time.Now(),
			}

			opportunities = append(opportunities, opportunity)
		}
	}

	return opportunities, nil
}

// calculateVolatility calcula la volatilidad para un símbolo
func (a *MarketDataAdapter) calculateVolatility(symbol string) float64 {
	a.dataMutex.RLock()
	defer a.dataMutex.RUnlock()

	prices := a.priceCache[symbol]
	if len(prices) < 2 {
		return 0
	}

	// Calcular retornos logarítmicos
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		returns[i-1] = math.Log(prices[i] / prices[i-1])
	}

	// Calcular desviación estándar de los retornos
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))

	return math.Sqrt(variance)
}

// calculateScore calcula una puntuación para la oportunidad
func calculateScore(volatility, volume, spreadPercentage float64) float64 {
	// Normalizar los valores
	volScore := math.Min(volatility/0.1, 1.0) // Normalizar volatilidad (max 10%)
	volScore = math.Max(volScore, 0.0)        // No permitir valores negativos

	volumeScore := math.Min(volume/1000000, 1.0) // Normalizar volumen (max 1M)
	volumeScore = math.Max(volumeScore, 0.0)     // No permitir valores negativos

	spreadScore := math.Min(spreadPercentage/1.0, 1.0) // Normalizar spread (max 1%)
	spreadScore = math.Max(spreadScore, 0.0)           // No permitir valores negativos

	// Ponderación de factores
	return (volScore * 0.4) + (volumeScore * 0.4) + (spreadScore * 0.2)
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
