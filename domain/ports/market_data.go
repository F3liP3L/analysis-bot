package ports

import (
	"context"
	"time"
)

// MarketData representa los datos del mercado en tiempo real
type MarketData struct {
	Symbol        string
	Price         float64
	Volume        float64
	BidPrice      float64
	AskPrice      float64
	Spread        float64
	Volatility    float64
	Timestamp     time.Time
}

// Opportunity representa una oportunidad de trading identificada
type Opportunity struct {
	Symbol        string
	EntryPrice    float64
	TargetPrice   float64
	StopLossPrice float64
	PotentialProfit float64
	Risk          float64
	Score         float64 // Puntuación basada en la "confianza" en la operación
	Timestamp     time.Time
}

// MarketDataPort define la interfaz para obtener datos del mercado
type MarketDataPort interface {
	// SubscribeToMarketData suscribe a actualizaciones en tiempo real para un par específico
	SubscribeToMarketData(ctx context.Context, symbol string) (<-chan MarketData, error)
	
	// GetAllSymbols obtiene todos los símbolos disponibles para trading
	GetAllSymbols(ctx context.Context) ([]string, error)
	
	// GetMarketData obtiene datos actuales del mercado para un símbolo específico
	GetMarketData(ctx context.Context, symbol string) (MarketData, error)
	
	// AnalyzeOpportunities analiza los datos de mercado para identificar oportunidades
	AnalyzeOpportunities(ctx context.Context, data []MarketData) ([]Opportunity, error)
} 