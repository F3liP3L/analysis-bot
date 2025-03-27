package binance

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/binancebot/domain/ports"
	"go.uber.org/zap"
)

// OrderExecutionAdapter implementa el puerto OrderExecutionPort para Binance
type OrderExecutionAdapter struct {
	client *binance.Client
	logger *zap.Logger
}

// NewOrderExecutionAdapter crea un nuevo adaptador para ejecución de órdenes en Binance
func NewOrderExecutionAdapter(apiKey, apiSecret string, logger *zap.Logger) *OrderExecutionAdapter {
	client := binance.NewClient(apiKey, apiSecret)

	return &OrderExecutionAdapter{
		client: client,
		logger: logger,
	}
}

// CreateOrder crea una nueva orden
func (a *OrderExecutionAdapter) CreateOrder(
	ctx context.Context,
	symbol string,
	side ports.OrderSide,
	orderType ports.OrderType,
	quantity float64,
	price float64,
	stopPrice float64,
) (ports.Order, error) {
	// Convertir los tipos de dominio a tipos de Binance
	binanceSide := binance.SideType(side)
	binanceOrderType := binance.OrderType(orderType)

	// Preparar la solicitud de orden
	quantityStr := strconv.FormatFloat(quantity, 'f', -1, 64)

	var priceStr string
	if price > 0 {
		priceStr = strconv.FormatFloat(price, 'f', -1, 64)
	}

	var stopPriceStr string
	if stopPrice > 0 {
		stopPriceStr = strconv.FormatFloat(stopPrice, 'f', -1, 64)
	}

	// Crear la orden según el tipo
	var createOrderService *binance.CreateOrderService
	switch orderType {
	case ports.OrderTypeLimit:
		createOrderService = a.client.NewCreateOrderService().
			Symbol(symbol).
			Side(binanceSide).
			Type(binanceOrderType).
			TimeInForce(binance.TimeInForceTypeGTC).
			Quantity(quantityStr).
			Price(priceStr)

	case ports.OrderTypeMarket:
		createOrderService = a.client.NewCreateOrderService().
			Symbol(symbol).
			Side(binanceSide).
			Type(binanceOrderType).
			Quantity(quantityStr)

	case ports.OrderTypeStop:
		createOrderService = a.client.NewCreateOrderService().
			Symbol(symbol).
			Side(binanceSide).
			Type(binance.OrderTypeStopLoss).
			Quantity(quantityStr).
			StopPrice(stopPriceStr)

	default:
		return ports.Order{}, fmt.Errorf("tipo de orden no soportado: %s", orderType)
	}

	// Ejecutar la orden
	res, err := createOrderService.Do(ctx)
	if err != nil {
		return ports.Order{}, fmt.Errorf("error al crear orden: %w", err)
	}

	// Convertir la respuesta al formato del dominio
	order, err := a.convertBinanceOrderToPortsOrder(res)
	if err != nil {
		return ports.Order{}, fmt.Errorf("error al convertir orden: %w", err)
	}

	return order, nil
}

// GetOrder obtiene información de una orden existente
func (a *OrderExecutionAdapter) GetOrder(
	ctx context.Context,
	symbol string,
	orderID int64,
) (ports.Order, error) {
	// Obtener la orden de Binance
	res, err := a.client.NewGetOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
	if err != nil {
		return ports.Order{}, fmt.Errorf("error al obtener orden: %w", err)
	}

	// Convertir la respuesta al formato del dominio
	order, err := a.convertBinanceOrderToPortsOrder(res)
	if err != nil {
		return ports.Order{}, fmt.Errorf("error al convertir orden: %w", err)
	}

	return order, nil
}

// CancelOrder cancela una orden existente
func (a *OrderExecutionAdapter) CancelOrder(
	ctx context.Context,
	symbol string,
	orderID int64,
) error {
	// Cancelar la orden en Binance
	_, err := a.client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("error al cancelar orden: %w", err)
	}

	return nil
}

// ListOpenOrders lista todas las órdenes abiertas
func (a *OrderExecutionAdapter) ListOpenOrders(
	ctx context.Context,
	symbol string,
) ([]ports.Order, error) {
	// Obtener órdenes abiertas de Binance
	res, err := a.client.NewListOpenOrdersService().
		Symbol(symbol).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("error al listar órdenes abiertas: %w", err)
	}

	// Convertir las respuestas al formato del dominio
	orders := make([]ports.Order, 0, len(res))
	for _, binanceOrder := range res {
		order, err := a.convertBinanceOrderToPortsOrder(binanceOrder)
		if err != nil {
			a.logger.Error("Error al convertir orden",
				zap.String("symbol", symbol),
				zap.Int64("orderID", binanceOrder.OrderID),
				zap.Error(err))
			continue
		}
		orders = append(orders, order)
	}

	return orders, nil
}

// GetAccount obtiene información de la cuenta
func (a *OrderExecutionAdapter) GetAccount(ctx context.Context) (ports.Account, error) {
	// Obtener información de la cuenta de Binance
	res, err := a.client.NewGetAccountService().Do(ctx)
	if err != nil {
		return ports.Account{}, fmt.Errorf("error al obtener información de la cuenta: %w", err)
	}

	// Convertir la respuesta al formato del dominio
	account := ports.Account{
		TotalBalance:     0, // Se calculará sumando los balances
		AvailableBalance: 0, // Se calculará sumando los balances disponibles
		Assets:           make(map[string]ports.AssetBalance),
		UpdateTime:       time.Unix(0, int64(res.UpdateTime)*int64(time.Millisecond)),
	}

	// Convertir los balances de activos
	for _, balance := range res.Balances {
		free, err := strconv.ParseFloat(balance.Free, 64)
		if err != nil {
			a.logger.Error("Error al parsear balance libre",
				zap.String("asset", balance.Asset), zap.Error(err))
			continue
		}

		locked, err := strconv.ParseFloat(balance.Locked, 64)
		if err != nil {
			a.logger.Error("Error al parsear balance bloqueado",
				zap.String("asset", balance.Asset), zap.Error(err))
			continue
		}

		// Solo incluir activos con balance
		if free > 0 || locked > 0 {
			account.Assets[balance.Asset] = ports.AssetBalance{
				Asset:  balance.Asset,
				Free:   free,
				Locked: locked,
			}

			// Calcular balance total y disponible (consideramos especialmente USDT)
			if balance.Asset == "USDT" {
				account.TotalBalance += free + locked
				account.AvailableBalance += free
			}
		}
	}

	return account, nil
}

// convertBinanceOrderToPortsOrder convierte una orden de Binance al formato del dominio
func (a *OrderExecutionAdapter) convertBinanceOrderToPortsOrder(
	binanceOrder interface{},
) (ports.Order, error) {
	var order ports.Order

	switch v := binanceOrder.(type) {
	case *binance.CreateOrderResponse:
		// Convertir CreateOrderResponse
		price, err := strconv.ParseFloat(v.Price, 64)
		if err != nil {
			return ports.Order{}, fmt.Errorf("error al parsear precio: %w", err)
		}

		origQty, err := strconv.ParseFloat(v.OrigQuantity, 64)
		if err != nil {
			return ports.Order{}, fmt.Errorf("error al parsear cantidad original: %w", err)
		}

		execQty, err := strconv.ParseFloat(v.ExecutedQuantity, 64)
		if err != nil {
			return ports.Order{}, fmt.Errorf("error al parsear cantidad ejecutada: %w", err)
		}

		order = ports.Order{
			Symbol:           v.Symbol,
			OrderID:          v.OrderID,
			ClientOrderID:    v.ClientOrderID,
			Price:            price,
			OriginalQuantity: origQty,
			ExecutedQuantity: execQty,
			Status:           ports.OrderStatus(v.Status),
			Type:             ports.OrderType(v.Type),
			Side:             ports.OrderSide(v.Side),
			Time:             time.Unix(0, v.TransactTime*int64(time.Millisecond)),
		}

	case *binance.Order:
		// Convertir Order
		price, err := strconv.ParseFloat(v.Price, 64)
		if err != nil {
			return ports.Order{}, fmt.Errorf("error al parsear precio: %w", err)
		}

		origQty, err := strconv.ParseFloat(v.OrigQuantity, 64)
		if err != nil {
			return ports.Order{}, fmt.Errorf("error al parsear cantidad original: %w", err)
		}

		execQty, err := strconv.ParseFloat(v.ExecutedQuantity, 64)
		if err != nil {
			return ports.Order{}, fmt.Errorf("error al parsear cantidad ejecutada: %w", err)
		}

		stopPrice := 0.0
		if v.StopPrice != "" {
			stopPrice, err = strconv.ParseFloat(v.StopPrice, 64)
			if err != nil {
				return ports.Order{}, fmt.Errorf("error al parsear stop price: %w", err)
			}
		}

		order = ports.Order{
			Symbol:           v.Symbol,
			OrderID:          v.OrderID,
			ClientOrderID:    v.ClientOrderID,
			Price:            price,
			OriginalQuantity: origQty,
			ExecutedQuantity: execQty,
			Status:           ports.OrderStatus(v.Status),
			Type:             ports.OrderType(v.Type),
			Side:             ports.OrderSide(v.Side),
			StopPrice:        stopPrice,
			Time:             time.Unix(0, v.Time*int64(time.Millisecond)),
		}

	default:
		return ports.Order{}, fmt.Errorf("tipo de orden no soportado: %T", binanceOrder)
	}

	return order, nil
}
