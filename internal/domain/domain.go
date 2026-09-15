// Package domain defines the values shared by market data and strategy code.
// Constructed quotes and intents must be validated before use.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Symbol identifies an instrument. Validation rejects blank symbols.
type Symbol string

// IntentID identifies a strategy decision. Retries must reuse the same ID and
// payload. Identity generation and deduplication belong to later components.
type IntentID string

// Quantity is a count of whole shares, not fractional shares.
type Quantity int64

// Validate requires at least one share.
func (q Quantity) Validate() error {
	if q <= 0 {
		return fmt.Errorf("quantity must be positive: %d", q)
	}
	return nil
}

// Side is a buy or sell intent; it does not authorize short selling.
type Side string

const (
	Buy  Side = "BUY"
	Sell Side = "SELL"
)

// Quote is a bid/ask observation at Timestamp. A nonzero time.Time represents
// a valid instant; no wall-clock freshness or timezone restriction is imposed.
type Quote struct {
	Symbol    Symbol
	Timestamp time.Time
	Bid       Price
	Ask       Price
}

func (q Quote) Validate() error {
	if strings.TrimSpace(string(q.Symbol)) == "" {
		return fmt.Errorf("quote symbol must not be blank")
	}
	if q.Timestamp.IsZero() {
		return fmt.Errorf("quote timestamp must not be zero")
	}
	if q.Bid <= 0 || q.Ask <= 0 {
		return fmt.Errorf("quote bid and ask must be positive")
	}
	if q.Bid > q.Ask {
		return fmt.Errorf("quote bid must not exceed ask")
	}
	return nil
}

// OrderIntent describes a strategy decision only, with no execution behavior.
type OrderIntent struct {
	IntentID  IntentID
	Symbol    Symbol
	Side      Side
	Quantity  Quantity
	Timestamp time.Time
}

func (i OrderIntent) Validate() error {
	if strings.TrimSpace(string(i.IntentID)) == "" {
		return fmt.Errorf("intent ID must not be blank")
	}
	if strings.TrimSpace(string(i.Symbol)) == "" {
		return fmt.Errorf("intent symbol must not be blank")
	}
	if i.Side != Buy && i.Side != Sell {
		return fmt.Errorf("invalid intent side %q", i.Side)
	}
	if err := i.Quantity.Validate(); err != nil {
		return fmt.Errorf("intent: %w", err)
	}
	if i.Timestamp.IsZero() {
		return fmt.Errorf("intent timestamp must not be zero")
	}
	return nil
}
