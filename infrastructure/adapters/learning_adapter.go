package adapters

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/binancebot/domain/ports"
	"go.uber.org/zap"
)

// LearningAdapter implementa el puerto LearningPort
type LearningAdapter struct {
	logger *zap.Logger
	
	// Almacenamiento de resultados de trades históricos
	resultsMutex      sync.RWMutex
	tradeResults      []ports.TradeResult
	
	// Caché de confianza por símbolo
	confidenceMutex   sync.RWMutex
	symbolConfidences map[string]ports.SymbolConfidence
	
	// Modelo de Q-Learning simplificado
	modelMutex        sync.RWMutex
	qValues           map[string]float64 // Mapeo de "estado-acción" a valor Q
	learningRate      float64
	discountFactor    float64
}

// NewLearningAdapter crea un nuevo adaptador para el módulo de aprendizaje
func NewLearningAdapter(
	logger *zap.Logger,
	learningRate float64,
	discountFactor float64,
) *LearningAdapter {
	return &LearningAdapter{
		logger:            logger,
		tradeResults:      make([]ports.TradeResult, 0, 1000),
		symbolConfidences: make(map[string]ports.SymbolConfidence),
		qValues:           make(map[string]float64),
		learningRate:      learningRate,
		discountFactor:    discountFactor,
	}
}

// RegisterTradeResult registra el resultado de un trade para el aprendizaje
func (a *LearningAdapter) RegisterTradeResult(
	ctx context.Context,
	result ports.TradeResult,
) error {
	a.resultsMutex.Lock()
	// Agregar el nuevo resultado
	a.tradeResults = append(a.tradeResults, result)
	// Limitar el historial a los últimos 1000 resultados
	if len(a.tradeResults) > 1000 {
		a.tradeResults = a.tradeResults[len(a.tradeResults)-1000:]
	}
	a.resultsMutex.Unlock()
	
	// Actualizar confianza para este símbolo
	a.updateSymbolConfidence(result.Symbol)
	
	// Actualizar modelo Q-Learning con este resultado
	stateAction := a.createStateAction(result)
	reward := a.calculateReward(result)
	
	a.modelMutex.Lock()
	oldQValue := a.qValues[stateAction]
	// Fórmula Q-Learning: Q(s,a) = Q(s,a) + lr * (reward + df * maxQ(s',a') - Q(s,a))
	// Simplificado para nuestro caso
	a.qValues[stateAction] = oldQValue + a.learningRate*(reward-oldQValue)
	a.modelMutex.Unlock()
	
	return nil
}

// UpdateModel actualiza el modelo de aprendizaje con los datos recientes
func (a *LearningAdapter) UpdateModel(ctx context.Context) error {
	a.resultsMutex.RLock()
	results := make([]ports.TradeResult, len(a.tradeResults))
	copy(results, a.tradeResults)
	a.resultsMutex.RUnlock()
	
	if len(results) == 0 {
		return nil // No hay datos para actualizar el modelo
	}
	
	// Actualizar confianza para todos los símbolos
	symbolsSet := make(map[string]bool)
	for _, result := range results {
		symbolsSet[result.Symbol] = true
	}
	
	for symbol := range symbolsSet {
		a.updateSymbolConfidence(symbol)
	}
	
	// Recalcular valores Q para todos los pares estado-acción
	for _, result := range results {
		stateAction := a.createStateAction(result)
		reward := a.calculateReward(result)
		
		a.modelMutex.Lock()
		oldQValue := a.qValues[stateAction]
		a.qValues[stateAction] = oldQValue + a.learningRate*(reward-oldQValue)
		a.modelMutex.Unlock()
	}
	
	return nil
}

// PredictTradeSuccess predice la probabilidad de éxito de un trade potencial
func (a *LearningAdapter) PredictTradeSuccess(
	ctx context.Context,
	features ports.ModelFeatures,
) (float64, error) {
	// Obtener confianza para este símbolo
	symbolConfidence, err := a.GetSymbolConfidence(ctx, features.Symbol)
	if err != nil {
		return 0.5, fmt.Errorf("error al obtener confianza para símbolo: %w", err)
	}
	
	// Si no hay suficientes datos históricos, usar un valor moderado
	if symbolConfidence.TotalTrades < 10 {
		return 0.5, nil
	}
	
	// Crear un estado-acción similar para buscar en el modelo Q
	stateAction := a.createStateActionFromFeatures(features)
	
	a.modelMutex.RLock()
	qValue, exists := a.qValues[stateAction]
	a.modelMutex.RUnlock()
	
	if !exists {
		// Si no existe este estado-acción, usar la tasa de éxito histórica como base
		return symbolConfidence.SuccessRate, nil
	}
	
	// Convertir el valor Q a una probabilidad (entre 0 y 1)
	// Los valores Q pueden variar, así que normalizamos usando sigmoid
	probability := 1.0 / (1.0 + math.Exp(-qValue))
	
	// Combinar con la tasa de éxito histórica para este símbolo
	combinedProbability := 0.7*probability + 0.3*symbolConfidence.SuccessRate
	
	return combinedProbability, nil
}

// GetSymbolConfidence obtiene la confianza del modelo en un símbolo específico
func (a *LearningAdapter) GetSymbolConfidence(
	ctx context.Context,
	symbol string,
) (ports.SymbolConfidence, error) {
	a.confidenceMutex.RLock()
	confidence, exists := a.symbolConfidences[symbol]
	a.confidenceMutex.RUnlock()
	
	if !exists {
		// Si no tenemos datos para este símbolo, calcular desde cero
		a.updateSymbolConfidence(symbol)
		
		a.confidenceMutex.RLock()
		confidence, exists = a.symbolConfidences[symbol]
		a.confidenceMutex.RUnlock()
		
		if !exists {
			// Si aún no existe, retornar valores por defecto
			return ports.SymbolConfidence{
				Symbol:           symbol,
				SuccessRate:      0.5, // 50% de éxito por defecto
				AverageProfitPct: 0,
				TotalTrades:      0,
				RecentTrades:     0,
				Confidence:       0.5,
			}, nil
		}
	}
	
	return confidence, nil
}

// GetAllSymbolConfidences obtiene la confianza del modelo en todos los símbolos
func (a *LearningAdapter) GetAllSymbolConfidences(
	ctx context.Context,
) ([]ports.SymbolConfidence, error) {
	a.confidenceMutex.RLock()
	confidences := make([]ports.SymbolConfidence, 0, len(a.symbolConfidences))
	for _, confidence := range a.symbolConfidences {
		confidences = append(confidences, confidence)
	}
	a.confidenceMutex.RUnlock()
	
	return confidences, nil
}

// GetOptimalParameters obtiene los parámetros óptimos de trading basados en el aprendizaje
func (a *LearningAdapter) GetOptimalParameters(
	ctx context.Context,
) (map[string]float64, error) {
	// Analizar datos históricos para obtener parámetros óptimos
	a.resultsMutex.RLock()
	results := make([]ports.TradeResult, len(a.tradeResults))
	copy(results, a.tradeResults)
	a.resultsMutex.RUnlock()
	
	if len(results) < 10 {
		// Si no hay suficientes datos, usar valores predeterminados
		return map[string]float64{
			"stopLossPercentage":      0.5,
			"takeProfitPercentage":    1.2,
			"tradeCapitalPercentage":  2.0,
			"maxSimultaneousTrades":   5,
			"maxDailyLossPercentage":  5.0,
			"reinvestmentPercentage":  100.0,
			"confidenceThreshold":     0.6,
			"minRiskRewardRatio":      1.5,
		}, nil
	}
	
	// Analizar resultados para determinar parámetros óptimos
	var totalProfit, successfulProfit, failedLoss float64
	var successfulTrades, failedTrades int
	
	for _, result := range results {
		if result.IsSuccessful {
			successfulTrades++
			successfulProfit += result.ProfitLoss
		} else {
			failedTrades++
			failedLoss += result.ProfitLoss // Será negativo
		}
		totalProfit += result.ProfitLoss
	}
	
	// Calcular parámetros óptimos basados en los resultados
	avgSuccessProfit := 0.0
	if successfulTrades > 0 {
		avgSuccessProfit = successfulProfit / float64(successfulTrades)
	}
	
	avgFailedLoss := 0.0
	if failedTrades > 0 {
		avgFailedLoss = failedLoss / float64(failedTrades)
	}
	
	// Ajustar los parámetros según el rendimiento
	optimalStopLoss := math.Abs(avgFailedLoss) * 0.8 // 80% del promedio de pérdidas
	if optimalStopLoss < 0.2 {
		optimalStopLoss = 0.2 // Mínimo 0.2%
	} else if optimalStopLoss > 2.0 {
		optimalStopLoss = 2.0 // Máximo 2.0%
	}
	
	optimalTakeProfit := avgSuccessProfit * 1.2 // 120% del promedio de ganancias
	if optimalTakeProfit < 0.5 {
		optimalTakeProfit = 0.5 // Mínimo 0.5%
	} else if optimalTakeProfit > 5.0 {
		optimalTakeProfit = 5.0 // Máximo 5.0%
	}
	
	// Optimizar el tamaño de posición basado en la rentabilidad
	optimalTradeCapital := 2.0 // Valor predeterminado
	if totalProfit > 0 {
		successRate := float64(successfulTrades) / float64(successfulTrades+failedTrades)
		if successRate > 0.7 {
			optimalTradeCapital = 3.0 // Aumentar si la tasa de éxito es alta
		} else if successRate < 0.4 {
			optimalTradeCapital = 1.0 // Disminuir si la tasa de éxito es baja
		}
	}
	
	// Optimizar número máximo de trades simultáneos
	optimalMaxTrades := 5 // Valor predeterminado
	if totalProfit > 0 && len(results) > 50 {
		optimalMaxTrades = 7 // Aumentar si el rendimiento es bueno y hay suficientes datos
	} else if totalProfit < 0 {
		optimalMaxTrades = 3 // Disminuir si el rendimiento es malo
	}
	
	return map[string]float64{
		"stopLossPercentage":      optimalStopLoss,
		"takeProfitPercentage":    optimalTakeProfit,
		"tradeCapitalPercentage":  optimalTradeCapital,
		"maxSimultaneousTrades":   float64(optimalMaxTrades),
		"maxDailyLossPercentage":  5.0, // Fijo por ahora
		"reinvestmentPercentage":  100.0, // Reinversión completa
		"confidenceThreshold":     0.6, // Umbral para ejecutar trades
		"minRiskRewardRatio":      1.5, // Ratio mínimo riesgo/beneficio
	}, nil
}

// updateSymbolConfidence actualiza la confianza para un símbolo específico
func (a *LearningAdapter) updateSymbolConfidence(symbol string) {
	a.resultsMutex.RLock()
	
	var totalTrades, successfulTrades, recentTrades, recentSuccessfulTrades int
	var totalProfit float64
	
	// Obtener la hora actual para calcular trades recientes (últimas 24h)
	now := time.Now()
	recentThreshold := now.Add(-24 * time.Hour)
	
	for _, result := range a.tradeResults {
		if result.Symbol != symbol {
			continue
		}
		
		totalTrades++
		totalProfit += result.ProfitLossPct
		
		if result.IsSuccessful {
			successfulTrades++
		}
		
		// Contar trades recientes
		if result.ExitTime.After(recentThreshold) {
			recentTrades++
			if result.IsSuccessful {
				recentSuccessfulTrades++
			}
		}
	}
	
	a.resultsMutex.RUnlock()
	
	// Calcular tasas y puntuación de confianza
	successRate := 0.5 // Valor predeterminado
	if totalTrades > 0 {
		successRate = float64(successfulTrades) / float64(totalTrades)
	}
	
	recentSuccessRate := 0.5 // Valor predeterminado
	if recentTrades > 0 {
		recentSuccessRate = float64(recentSuccessfulTrades) / float64(recentTrades)
	}
	
	averageProfitPct := 0.0
	if totalTrades > 0 {
		averageProfitPct = totalProfit / float64(totalTrades)
	}
	
	// Calcular puntuación de confianza ponderando más los resultados recientes
	confidenceScore := 0.5 // Valor predeterminado
	if totalTrades > 0 {
		if recentTrades > 0 {
			confidenceScore = 0.3*successRate + 0.7*recentSuccessRate
		} else {
			confidenceScore = successRate
		}
		
		// Ajustar por el beneficio promedio
		if averageProfitPct > 0 {
			factor := math.Min(averageProfitPct/2.0, 0.2) // Máximo ajuste de 0.2
			confidenceScore += factor
		} else if averageProfitPct < 0 {
			factor := math.Min(math.Abs(averageProfitPct)/2.0, 0.2) // Máximo ajuste de 0.2
			confidenceScore -= factor
		}
		
		// Asegurar que la puntuación esté entre 0 y 1
		confidenceScore = math.Max(0.0, math.Min(1.0, confidenceScore))
	}
	
	// Actualizar la caché de confianza
	confidence := ports.SymbolConfidence{
		Symbol:           symbol,
		SuccessRate:      successRate,
		AverageProfitPct: averageProfitPct,
		TotalTrades:      totalTrades,
		RecentTrades:     recentTrades,
		Confidence:       confidenceScore,
	}
	
	a.confidenceMutex.Lock()
	a.symbolConfidences[symbol] = confidence
	a.confidenceMutex.Unlock()
}

// createStateAction crea una clave para el modelo Q-Learning basado en un resultado de trade
func (a *LearningAdapter) createStateAction(result ports.TradeResult) string {
	timeOfDay := result.EntryTime.Format("15")
	dayOfWeek := result.EntryTime.Weekday().String()
	
	// Crear una clave compuesta que represente el estado y la acción
	return fmt.Sprintf("%s:%s:%s:%.2f", result.Symbol, timeOfDay, dayOfWeek, result.ProfitLossPct)
}

// createStateActionFromFeatures crea una clave para el modelo Q-Learning basado en características
func (a *LearningAdapter) createStateActionFromFeatures(features ports.ModelFeatures) string {
	timeOfDay := features.TimeOfDay[:2] // Solo usar la hora
	dayOfWeek := features.DayOfWeek
	
	// Crear una clave compuesta similar a la de los resultados
	return fmt.Sprintf("%s:%s:%s:%.2f", features.Symbol, timeOfDay, dayOfWeek, features.RecentSuccess)
}

// calculateReward calcula la recompensa para el modelo Q-Learning basado en un resultado de trade
func (a *LearningAdapter) calculateReward(result ports.TradeResult) float64 {
	// Conversión simple: ganancia porcentual como recompensa
	return result.ProfitLossPct
} 