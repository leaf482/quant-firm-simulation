// Package strategy converts quotes into educational trading intents only.
package strategy

import (
	"fmt"
	"math"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// PriceMovement compares consecutive rounded midpoints for one symbol.
// The first valid quote selects the symbol. Its zero value is ready to use.
// IDs are unique within this instance's run, not across independent runs.
type PriceMovement struct {
	symbol   domain.Symbol
	previous domain.Price
	sequence uint64
}

func NewPriceMovement() *PriceMovement { return &PriceMovement{} }

// OnQuote returns nil when the first quote arrives or its midpoint is unchanged.
// Rising/falling midpoints produce BUY/SELL intents for one share, respectively.
// Invalid quotes and symbol changes return an error without changing state.
func (s *PriceMovement) OnQuote(q domain.Quote) (*domain.OrderIntent, error) {
	if err := q.Validate(); err != nil {
		return nil, fmt.Errorf("price movement quote: %w", err)
	}
	if s.symbol != "" && q.Symbol != s.symbol {
		return nil, fmt.Errorf("price movement: expected symbol %q, got %q", s.symbol, q.Symbol)
	}
	// Valid quotes satisfy 0 < bid <= ask. This difference-based midpoint
	// cannot overflow int64, unlike (bid+ask)/2. Division rounds down to
	// $0.0001 units, so half-unit changes can produce no signal.
	midpoint := q.Bid + (q.Ask-q.Bid)/2
	if s.symbol == "" {
		s.symbol, s.previous = q.Symbol, midpoint
		return nil, nil
	}
	if midpoint == s.previous {
		s.previous = midpoint
		return nil, nil
	}
	side := domain.Buy
	if midpoint < s.previous {
		side = domain.Sell
	}
	if s.sequence == math.MaxUint64 {
		return nil, fmt.Errorf("price movement: intent sequence exhausted")
	}
	intent := domain.OrderIntent{
		IntentID: domain.IntentID(fmt.Sprintf("price-movement-%d", s.sequence+1)),
		Symbol:   q.Symbol, Side: side, Quantity: 1, Timestamp: q.Timestamp,
	}
	if err := intent.Validate(); err != nil {
		return nil, fmt.Errorf("price movement intent: %w", err)
	}
	s.sequence++
	s.previous = midpoint
	return &intent, nil
}
