package ports

import (
	"context"
	"time"
)

// OrderType define el tipo de orden
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeStop   OrderType = "STOP_LOSS"
)

// OrderSide define el lado de la orden (compra/venta)
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// OrderStatus define el estado de la orden
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
)

// Order representa una orden de trading
type Order struct {
	Symbol           string
	OrderID          int64
	ClientOrderID    string
	Price            float64
	OriginalQuantity float64
	ExecutedQuantity float64
	Status           OrderStatus
	Type             OrderType
	Side             OrderSide
	StopPrice        float64
	Time             time.Time
}

// Trade representa un trade completado
type Trade struct {
	Symbol          string
	ID              int64
	OrderID         int64
	Price           float64
	Quantity        float64
	Commission      float64
	CommissionAsset string
	Time            time.Time
	IsBuyer         bool
	IsMaker         bool
}

// Account representa la información de la cuenta
type Account struct {
	TotalBalance      float64
	AvailableBalance  float64
	Assets            map[string]AssetBalance
	UpdateTime        time.Time
}

// AssetBalance representa el balance de un activo
type AssetBalance struct {
	Asset  string
	Free   float64
	Locked float64
}

// OrderExecutionPort define la interfaz para la ejecución de órdenes
type OrderExecutionPort interface {
	// CreateOrder crea una nueva orden
	CreateOrder(ctx context.Context, symbol string, side OrderSide, orderType OrderType, 
		quantity float64, price float64, stopPrice float64) (Order, error)
	
	// GetOrder obtiene información de una orden existente
	GetOrder(ctx context.Context, symbol string, orderID int64) (Order, error)
	
	// CancelOrder cancela una orden existente
	CancelOrder(ctx context.Context, symbol string, orderID int64) error
	
	// ListOpenOrders lista todas las órdenes abiertas
	ListOpenOrders(ctx context.Context, symbol string) ([]Order, error)
	
	// GetAccount obtiene información de la cuenta
	GetAccount(ctx context.Context) (Account, error)
} 