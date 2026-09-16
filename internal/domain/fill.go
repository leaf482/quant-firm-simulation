package domain

import (
	"fmt"
	"strings"
	"time"
)

type FillID string

// Fill records a complete simulated execution at a market-data timestamp.
type Fill struct {
	FillID    FillID
	OrderID   OrderID
	Symbol    Symbol
	Side      Side
	Quantity  Quantity
	Price     Price
	Timestamp time.Time
}

func (f Fill) Validate() error {
	if strings.TrimSpace(string(f.FillID)) == "" {
		return fmt.Errorf("fill ID must not be blank")
	}
	if strings.TrimSpace(string(f.OrderID)) == "" {
		return fmt.Errorf("fill order ID must not be blank")
	}
	if strings.TrimSpace(string(f.Symbol)) == "" {
		return fmt.Errorf("fill symbol must not be blank")
	}
	if f.Side != Buy && f.Side != Sell {
		return fmt.Errorf("invalid fill side %q", f.Side)
	}
	if err := f.Quantity.Validate(); err != nil {
		return fmt.Errorf("fill: %w", err)
	}
	if f.Price <= 0 {
		return fmt.Errorf("fill price must be positive")
	}
	if f.Timestamp.IsZero() {
		return fmt.Errorf("fill timestamp must not be zero")
	}
	return nil
}
