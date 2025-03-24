package adapters

import (
	"context"
	"fmt"
	"sync"

	"github.com/binancebot/domain/ports"
	"go.uber.org/zap"
)

// RiskManagementAdapter implementa el puerto RiskManagementPort
type RiskManagementAdapter struct {
	logger *zap.Logger
	
	// Parámetros de riesgo
	paramsMutex       sync.RWMutex
	riskParams        ports.RiskParameters
	
	// Seguimiento de posiciones abiertas
	positionsMutex    sync.RWMutex
	openPositions     map[string]ports.PositionRisk
}

// NewRiskManagementAdapter crea un nuevo adaptador para gestión de riesgos
func NewRiskManagementAdapter(
	logger *zap.Logger,
	initialParams ports.RiskParameters,
) *RiskManagementAdapter {
	return &RiskManagementAdapter{
		logger:        logger,
		riskParams:    initialParams,
		openPositions: make(map[string]ports.PositionRisk),
	}
}

// CalculatePositionSize calcula el tamaño óptimo de posición para un trade
func (a *RiskManagementAdapter) CalculatePositionSize(
	ctx context.Context,
	symbol string,
	availableCapital float64,
) (float64, error) {
	a.paramsMutex.RLock()
	tradeCapitalPercentage := a.riskParams.TradeCapitalPercentage
	a.paramsMutex.RUnlock()
	
	// Calcular tamaño de posición basado en el porcentaje del capital disponible
	positionSize := availableCapital * (tradeCapitalPercentage / 100.0)
	
	// Implementar reglas adicionales para ajustar el tamaño de posición
	// Por ejemplo, limitar a un valor máximo
	if positionSize > 100.0 {
		positionSize = 100.0
	}
	
	return positionSize, nil
}

// ValidateTrade valida si un trade potencial cumple con los parámetros de riesgo
func (a *RiskManagementAdapter) ValidateTrade(
	ctx context.Context,
	opportunity ports.Opportunity,
	availableCapital float64,
) (bool, string, error) {
	// Verificar si ya tenemos una posición abierta para este símbolo
	a.positionsMutex.RLock()
	_, exists := a.openPositions[opportunity.Symbol]
	a.positionsMutex.RUnlock()
	
	if exists {
		return false, "Ya existe una posición abierta para este símbolo", nil
	}
	
	// Verificar si el riesgo/beneficio es aceptable
	riskRewardRatio := opportunity.PotentialProfit / opportunity.Risk
	if riskRewardRatio < 1.5 {
		return false, fmt.Sprintf("Ratio riesgo/beneficio insuficiente: %.2f", riskRewardRatio), nil
	}
	
	// Verificar si el potencial de ganancia es suficiente
	if opportunity.PotentialProfit < 1.0 {
		return false, fmt.Sprintf("Potencial de ganancia insuficiente: %.2f%%", opportunity.PotentialProfit), nil
	}
	
	// Verificar si el riesgo es aceptable
	if opportunity.Risk > 2.0 {
		return false, fmt.Sprintf("Riesgo demasiado alto: %.2f%%", opportunity.Risk), nil
	}
	
	// Verificar si el capital disponible es suficiente
	a.paramsMutex.RLock()
	maxSimultaneousTrades := a.riskParams.MaxSimultaneousTrades
	a.paramsMutex.RUnlock()
	
	a.positionsMutex.RLock()
	openPositionsCount := len(a.openPositions)
	a.positionsMutex.RUnlock()
	
	if openPositionsCount >= maxSimultaneousTrades {
		return false, fmt.Sprintf("Número máximo de trades simultáneos alcanzado: %d", maxSimultaneousTrades), nil
	}
	
	// Verificar el portafolio global
	portfolioRisk, err := a.EvaluatePortfolioRisk(ctx)
	if err != nil {
		return false, "", fmt.Errorf("error al evaluar riesgo del portafolio: %w", err)
	}
	
	// Limitar el riesgo total del portafolio
	if portfolioRisk.TotalRisk > 10.0 {
		return false, "Riesgo total del portafolio demasiado alto", nil
	}
	
	return true, "", nil
}

// CalculateStopLoss calcula el precio de stop-loss para una posición
func (a *RiskManagementAdapter) CalculateStopLoss(
	ctx context.Context,
	entryPrice float64,
	side ports.OrderSide,
) float64 {
	a.paramsMutex.RLock()
	stopLossPercentage := a.riskParams.StopLossPercentage
	a.paramsMutex.RUnlock()
	
	if side == ports.OrderSideBuy {
		// Para una posición larga, el stop-loss está por debajo del precio de entrada
		return entryPrice * (1 - stopLossPercentage/100.0)
	} else {
		// Para una posición corta, el stop-loss está por encima del precio de entrada
		return entryPrice * (1 + stopLossPercentage/100.0)
	}
}

// CalculateTakeProfit calcula el precio de take-profit para una posición
func (a *RiskManagementAdapter) CalculateTakeProfit(
	ctx context.Context,
	entryPrice float64,
	side ports.OrderSide,
) float64 {
	a.paramsMutex.RLock()
	takeProfitPercentage := a.riskParams.TakeProfitPercentage
	a.paramsMutex.RUnlock()
	
	if side == ports.OrderSideBuy {
		// Para una posición larga, el take-profit está por encima del precio de entrada
		return entryPrice * (1 + takeProfitPercentage/100.0)
	} else {
		// Para una posición corta, el take-profit está por debajo del precio de entrada
		return entryPrice * (1 - takeProfitPercentage/100.0)
	}
}

// EvaluatePortfolioRisk evalúa el riesgo actual del portafolio
func (a *RiskManagementAdapter) EvaluatePortfolioRisk(ctx context.Context) (ports.PortfolioRisk, error) {
	a.positionsMutex.RLock()
	defer a.positionsMutex.RUnlock()
	
	portfolioRisk := ports.PortfolioRisk{
		TotalPositions:       len(a.openPositions),
		TotalInvestedCapital: 0,
		TotalRisk:            0,
		TotalUnrealizedPnL:   0,
		Positions:            make([]ports.PositionRisk, 0, len(a.openPositions)),
	}
	
	// Calcular el riesgo total del portafolio
	for _, position := range a.openPositions {
		portfolioRisk.Positions = append(portfolioRisk.Positions, position)
		portfolioRisk.TotalInvestedCapital += position.EntryPrice * position.Quantity
		portfolioRisk.TotalUnrealizedPnL += position.UnrealizedProfitLoss
		
		// Calcular riesgo total como la suma del valor en riesgo de cada posición
		riskAmount := (position.EntryPrice - position.StopLossPrice) * position.Quantity
		if riskAmount < 0 {
			riskAmount = -riskAmount
		}
		portfolioRisk.TotalRisk += riskAmount
	}
	
	return portfolioRisk, nil
}

// GetRiskParameters obtiene los parámetros actuales de gestión de riesgos
func (a *RiskManagementAdapter) GetRiskParameters(ctx context.Context) (ports.RiskParameters, error) {
	a.paramsMutex.RLock()
	defer a.paramsMutex.RUnlock()
	
	return a.riskParams, nil
}

// UpdateRiskParameters actualiza los parámetros de gestión de riesgos
func (a *RiskManagementAdapter) UpdateRiskParameters(
	ctx context.Context,
	params ports.RiskParameters,
) error {
	// Validar parámetros
	if params.StopLossPercentage <= 0 || params.TakeProfitPercentage <= 0 {
		return fmt.Errorf("porcentajes de stop-loss y take-profit deben ser positivos")
	}
	
	if params.MaxSimultaneousTrades <= 0 {
		return fmt.Errorf("número máximo de trades simultáneos debe ser positivo")
	}
	
	if params.TradeCapitalPercentage <= 0 || params.TradeCapitalPercentage > 100 {
		return fmt.Errorf("porcentaje de capital por trade debe estar entre 0 y 100")
	}
	
	// Actualizar parámetros
	a.paramsMutex.Lock()
	a.riskParams = params
	a.paramsMutex.Unlock()
	
	return nil
}

// UpdatePosition actualiza una posición existente o agrega una nueva (método para uso interno)
func (a *RiskManagementAdapter) UpdatePosition(position ports.PositionRisk) {
	a.positionsMutex.Lock()
	defer a.positionsMutex.Unlock()
	
	a.openPositions[position.Symbol] = position
}

// RemovePosition elimina una posición (método para uso interno)
func (a *RiskManagementAdapter) RemovePosition(symbol string) {
	a.positionsMutex.Lock()
	defer a.positionsMutex.Unlock()
	
	delete(a.openPositions, symbol)
} 