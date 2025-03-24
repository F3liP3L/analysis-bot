package ports

import (
	"context"
	"time"
)

// TradeResult representa el resultado de un trade
type TradeResult struct {
	Symbol         string
	EntryPrice     float64
	ExitPrice      float64
	Quantity       float64
	Side           OrderSide
	EntryTime      time.Time
	ExitTime       time.Time
	ProfitLoss     float64
	ProfitLossPct  float64
	Commission     float64
	ExecutionSpeed time.Duration
	IsSuccessful   bool
	Reason         string // Razón del éxito o fracaso
}

// ModelFeatures representa las características para el modelo de aprendizaje
type ModelFeatures struct {
	Symbol         string
	Price          float64
	Volume         float64
	Volatility     float64
	Spread         float64
	MarketTrend    float64
	TimeOfDay      string
	DayOfWeek      string
	RecentSuccess  float64 // Tasa de éxito reciente para este símbolo
}

// SymbolConfidence representa la confianza del modelo en un símbolo específico
type SymbolConfidence struct {
	Symbol           string
	SuccessRate      float64
	AverageProfitPct float64
	TotalTrades      int
	RecentTrades     int
	Confidence       float64
}

// LearningPort define la interfaz para el módulo de aprendizaje
type LearningPort interface {
	// RegisterTradeResult registra el resultado de un trade para el aprendizaje
	RegisterTradeResult(ctx context.Context, result TradeResult) error
	
	// UpdateModel actualiza el modelo de aprendizaje con los datos recientes
	UpdateModel(ctx context.Context) error
	
	// PredictTradeSuccess predice la probabilidad de éxito de un trade potencial
	PredictTradeSuccess(ctx context.Context, features ModelFeatures) (float64, error)
	
	// GetSymbolConfidence obtiene la confianza del modelo en un símbolo específico
	GetSymbolConfidence(ctx context.Context, symbol string) (SymbolConfidence, error)
	
	// GetAllSymbolConfidences obtiene la confianza del modelo en todos los símbolos
	GetAllSymbolConfidences(ctx context.Context) ([]SymbolConfidence, error)
	
	// GetOptimalParameters obtiene los parámetros óptimos de trading basados en el aprendizaje
	GetOptimalParameters(ctx context.Context) (map[string]float64, error)
} 