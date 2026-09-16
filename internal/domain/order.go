package domain

import (
	"fmt"
	"strings"
	"time"
)

type OrderID string
type OrderStatus string

const (
	OrderNew       OrderStatus = "NEW"
	OrderSubmitted OrderStatus = "SUBMITTED"
	OrderCancelled OrderStatus = "CANCELLED"
	OrderRejected  OrderStatus = "REJECTED"
)

func (s OrderStatus) Validate() error {
	switch s {
	case OrderNew, OrderSubmitted, OrderCancelled, OrderRejected:
		return nil
	default:
		return fmt.Errorf("invalid order status %q", s)
	}
}

// Order describes a managed order, not a broker request or execution.
type Order struct {
	OrderID   OrderID
	IntentID  IntentID
	Symbol    Symbol
	Side      Side
	Quantity  Quantity
	Status    OrderStatus
	CreatedAt time.Time
}

func (o Order) Validate() error {
	if strings.TrimSpace(string(o.OrderID)) == "" {
		return fmt.Errorf("order ID must not be blank")
	}
	if err := o.Status.Validate(); err != nil {
		return err
	}
	intent := OrderIntent{IntentID: o.IntentID, Symbol: o.Symbol, Side: o.Side, Quantity: o.Quantity, Timestamp: o.CreatedAt}
	if err := intent.Validate(); err != nil {
		return fmt.Errorf("order: %w", err)
	}
	return nil
}
