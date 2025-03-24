package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/binancebot/domain/core"
	"github.com/binancebot/domain/ports"
	"github.com/binancebot/infrastructure/adapters"
	"github.com/binancebot/infrastructure/binance"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Configurar logger
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.OutputPaths = []string{"stdout", "logs/bot.log"}
	logger, err := config.Build()
	if err != nil {
		fmt.Printf("Error al crear logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Cargar variables de entorno
	err = godotenv.Load()
	if err != nil {
		logger.Fatal("Error al cargar archivo .env", zap.Error(err))
	}

	// Obtener configuración desde variables de entorno
	apiKey := os.Getenv("API_KEY")
	apiSecret := os.Getenv("API_SECRET")
	minProfitUSD, _ := strconv.ParseFloat(os.Getenv("MIN_PROFIT_USD"), 64)
	dailyTradeTarget, _ := strconv.Atoi(os.Getenv("DAILY_TRADE_TARGET"))
	reinvestmentPercentage, _ := strconv.ParseFloat(os.Getenv("REINVESTMENT_PERCENTAGE"), 64)
	maxSimultaneousTrades, _ := strconv.Atoi(os.Getenv("MAX_SIMULTANEOUS_TRADES"))
	tradeSizePercentage, _ := strconv.ParseFloat(os.Getenv("TRADE_SIZE_PERCENTAGE"), 64)
	stopLossPercentage, _ := strconv.ParseFloat(os.Getenv("STOP_LOSS_PERCENTAGE"), 64)
	takeProfitPercentage, _ := strconv.ParseFloat(os.Getenv("TAKE_PROFIT_PERCENTAGE"), 64)
	volatilityThreshold, _ := strconv.ParseFloat(os.Getenv("VOLATILITY_THRESHOLD"), 64)
	volumeThreshold, _ := strconv.ParseFloat(os.Getenv("VOLUME_THRESHOLD"), 64)
	minSpreadPercentage, _ := strconv.ParseFloat(os.Getenv("MIN_SPREAD_PERCENTAGE"), 64)

	// Inicializar parámetros de riesgo
	riskParams := ports.RiskParameters{
		MaxSimultaneousTrades:  maxSimultaneousTrades,
		TradeCapitalPercentage: tradeSizePercentage,
		StopLossPercentage:     stopLossPercentage,
		TakeProfitPercentage:   takeProfitPercentage,
		MaxDailyLossPercentage: 5.0, // Valor por defecto
		ReinvestmentPercentage: reinvestmentPercentage,
	}

	// Crear adaptadores
	marketDataAdapter := binance.NewMarketDataAdapter(
		apiKey, 
		apiSecret, 
		logger, 
		volatilityThreshold, 
		volumeThreshold, 
		minSpreadPercentage,
	)
	
	orderExecutionAdapter := binance.NewOrderExecutionAdapter(
		apiKey, 
		apiSecret, 
		logger,
	)
	
	riskManagementAdapter := adapters.NewRiskManagementAdapter(
		logger, 
		riskParams,
	)
	
	learningAdapter := adapters.NewLearningAdapter(
		logger, 
		0.1,  // Tasa de aprendizaje
		0.95, // Factor de descuento
	)

	// Crear servicio de trading
	tradingService := core.NewTradingService(
		marketDataAdapter,
		orderExecutionAdapter,
		riskManagementAdapter,
		learningAdapter,
		logger,
		dailyTradeTarget,
		minProfitUSD,
	)

	// Manejar señales de terminación
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Canal para recibir señales de interrupción
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Iniciar el bot en una goroutine
	go func() {
		if err := tradingService.Start(ctx); err != nil {
			logger.Error("Error en el servicio de trading", zap.Error(err))
			cancel()
		}
	}()

	// Esperar señal de interrupción
	sig := <-sigCh
	logger.Info("Recibida señal de terminación", zap.String("signal", sig.String()))
	cancel()
	logger.Info("Bot detenido correctamente")
} 