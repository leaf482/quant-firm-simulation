// Package risk performs side-effect-free checks against a supplied account snapshot.
package risk

import (
	"fmt"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// Limits applies to BUY intents. SELL intents may reduce positions above limits.
// MaxOrderNotional is total money in $0.0001 units, not a per-share price.
type Limits struct {
	MaxOrderNotional domain.Money
	MaxPosition      domain.Quantity
}

func (l Limits) validate() error {
	if l.MaxOrderNotional <= 0 {
		return fmt.Errorf("maximum order notional must be positive")
	}
	if l.MaxPosition <= 0 {
		return fmt.Errorf("maximum position must be positive")
	}
	return nil
}

// Checker holds immutable limits. Construct it with NewChecker.
type Checker struct{ limits Limits }

func NewChecker(limits Limits) (*Checker, error) {
	if err := limits.validate(); err != nil {
		return nil, fmt.Errorf("risk limits: %w", err)
	}
	return &Checker{limits: limits}, nil
}

// Check returns nil for approval, otherwise the first rejection reason.
// availableCash is total cash in $0.0001 units; currentPosition is held shares
// of the quote's symbol. Both may be zero but cannot be negative.
// Approval does not reserve resources or guarantee a future execution price.
func (c *Checker) Check(intent domain.OrderIntent, quote domain.Quote, availableCash domain.Money, currentPosition domain.Quantity) error {
	if err := c.limits.validate(); err != nil {
		return fmt.Errorf("risk limits: %w", err)
	}
	if err := intent.Validate(); err != nil {
		return fmt.Errorf("risk intent: %w", err)
	}
	if err := quote.Validate(); err != nil {
		return fmt.Errorf("risk quote: %w", err)
	}
	if intent.Symbol != quote.Symbol {
		return fmt.Errorf("risk: intent symbol %q does not match quote symbol %q", intent.Symbol, quote.Symbol)
	}
	if availableCash < 0 {
		return fmt.Errorf("risk: available cash must not be negative")
	}
	if currentPosition < 0 {
		return fmt.Errorf("risk: current position must not be negative")
	}
	price := quote.Ask
	if intent.Side == domain.Sell {
		price = quote.Bid
	}
	notional, err := domain.Notional(price, intent.Quantity)
	if err != nil {
		return fmt.Errorf("risk: estimated order %w", err)
	}
	if intent.Side == domain.Sell {
		if intent.Quantity > currentPosition {
			return fmt.Errorf("risk: insufficient holdings: need %d, have %d", intent.Quantity, currentPosition)
		}
		return nil
	}
	if notional > availableCash {
		return fmt.Errorf("risk: insufficient cash: need %s, have %s", notional, availableCash)
	}
	if notional > c.limits.MaxOrderNotional {
		return fmt.Errorf("risk: estimated notional %s exceeds maximum order notional %s", notional, c.limits.MaxOrderNotional)
	}
	// Avoid adding quantities, which could overflow. Subtraction is safe after
	// checking that the nonnegative current position is within the limit.
	if currentPosition > c.limits.MaxPosition || intent.Quantity > c.limits.MaxPosition-currentPosition {
		return fmt.Errorf("risk: resulting position exceeds maximum position %d", c.limits.MaxPosition)
	}
	return nil
}
