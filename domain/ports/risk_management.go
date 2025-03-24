package ports

import (
	"context"
)

// RiskParameters define los parámetros de gestión de riesgos
type RiskParameters struct {
	MaxSimultaneousTrades   int     // Número máximo de operaciones simultáneas
	TradeCapitalPercentage  float64 // Porcentaje de capital por operación
	StopLossPercentage      float64 // Porcentaje de stop-loss
	TakeProfitPercentage    float64 // Porcentaje de take-profit
	MaxDailyLossPercentage  float64 // Porcentaje máximo de pérdida diaria
	ReinvestmentPercentage  float64 // Porcentaje de reinversión de ganancias
}

// PositionRisk representa el riesgo actual de una posición
type PositionRisk struct {
	Symbol               string
	EntryPrice           float64
	CurrentPrice         float64
	Quantity             float64
	StopLossPrice        float64
	TakeProfitPrice      float64
	UnrealizedProfitLoss float64
	RiskRewardRatio      float64
}

// PortfolioRisk representa el riesgo total del portafolio
type PortfolioRisk struct {
	TotalPositions       int
	TotalInvestedCapital float64
	TotalRisk            float64
	TotalUnrealizedPnL   float64
	DailyPnL             float64
	Positions            []PositionRisk
}

// RiskManagementPort define la interfaz para la gestión de riesgos
type RiskManagementPort interface {
	// CalculatePositionSize calcula el tamaño óptimo de posición para un trade
	CalculatePositionSize(ctx context.Context, symbol string, availableCapital float64) (float64, error)
	
	// ValidateTrade valida si un trade potencial cumple con los parámetros de riesgo
	ValidateTrade(ctx context.Context, opportunity Opportunity, availableCapital float64) (bool, string, error)
	
	// CalculateStopLoss calcula el precio de stop-loss para una posición
	CalculateStopLoss(ctx context.Context, entryPrice float64, side OrderSide) float64
	
	// CalculateTakeProfit calcula el precio de take-profit para una posición
	CalculateTakeProfit(ctx context.Context, entryPrice float64, side OrderSide) float64
	
	// EvaluatePortfolioRisk evalúa el riesgo actual del portafolio
	EvaluatePortfolioRisk(ctx context.Context) (PortfolioRisk, error)
	
	// GetRiskParameters obtiene los parámetros actuales de gestión de riesgos
	GetRiskParameters(ctx context.Context) (RiskParameters, error)
	
	// UpdateRiskParameters actualiza los parámetros de gestión de riesgos
	UpdateRiskParameters(ctx context.Context, params RiskParameters) error
} 