// Package portfolio maintains a single-symbol, long-only account in memory.
package portfolio

import (
	"fmt"
	"math"
	"strings"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

// Portfolio is single-threaded. Construct it with New; its zero value is not usable.
type Portfolio struct {
	symbol      domain.Symbol
	initialCash domain.Money
	cash        domain.Money
	position    domain.Quantity
	applied     map[domain.FillID]domain.Fill
}

func New(symbol domain.Symbol, initialCash domain.Money) (*Portfolio, error) {
	if strings.TrimSpace(string(symbol)) == "" {
		return nil, fmt.Errorf("portfolio symbol must not be blank")
	}
	if initialCash < 0 {
		return nil, fmt.Errorf("initial cash must not be negative")
	}
	return &Portfolio{symbol: symbol, initialCash: initialCash, cash: initialCash, applied: make(map[domain.FillID]domain.Fill)}, nil
}

// Apply validates and calculates before committing any changes, including the
// deduplication record. Identical retries succeed without changing state;
// conflicting reuse of FillID fails. Timestamps are compared as instants.
func (p *Portfolio) Apply(fill domain.Fill) error {
	if p.applied == nil {
		return fmt.Errorf("portfolio must be constructed with New")
	}
	if err := fill.Validate(); err != nil {
		return fmt.Errorf("portfolio fill: %w", err)
	}
	if fill.Symbol != p.symbol {
		return fmt.Errorf("portfolio: expected symbol %q, got %q", p.symbol, fill.Symbol)
	}
	if previous, ok := p.applied[fill.FillID]; ok {
		if previous.OrderID != fill.OrderID || previous.Symbol != fill.Symbol || previous.Side != fill.Side || previous.Quantity != fill.Quantity || previous.Price != fill.Price || !previous.Timestamp.Equal(fill.Timestamp) {
			return fmt.Errorf("portfolio: conflicting payload for fill %q", fill.FillID)
		}
		return nil
	}
	value, err := domain.Notional(fill.Price, fill.Quantity)
	if err != nil {
		return fmt.Errorf("portfolio: %w", err)
	}
	cash, position := p.cash, p.position
	if fill.Side == domain.Buy {
		if value > cash {
			return fmt.Errorf("portfolio: insufficient cash: need %s, have %s", value, cash)
		}
		if fill.Quantity > math.MaxInt64-position {
			return fmt.Errorf("portfolio: position overflows int64")
		}
		cash -= value
		position += fill.Quantity
	} else {
		if fill.Quantity > position {
			return fmt.Errorf("portfolio: insufficient holdings: need %d, have %d", fill.Quantity, position)
		}
		if value > math.MaxInt64-cash {
			return fmt.Errorf("portfolio: cash overflows int64")
		}
		cash += value
		position -= fill.Quantity
	}
	p.cash, p.position = cash, position
	p.applied[fill.FillID] = fill
	return nil
}

// Snapshot is a read-only valuation in $0.0001 units, except Position (shares).
// PnL is total equity minus initial cash, without a realized/unrealized split.
type Snapshot struct {
	Cash        domain.Money
	Position    domain.Quantity
	MarketValue domain.Money
	Equity      domain.Money
	PnL         domain.Money
}

// State returns cash and shares by value, without requiring a market mark.
// The portfolio must have been constructed with New.
func (p *Portfolio) State() (domain.Money, domain.Quantity) { return p.cash, p.position }

// Snapshot values holdings at the supplied quote's bid. It never changes account
// state or stores a mark; quote selection and freshness belong to the caller.
func (p *Portfolio) Snapshot(quote domain.Quote) (Snapshot, error) {
	if p.applied == nil {
		return Snapshot{}, fmt.Errorf("portfolio must be constructed with New")
	}
	if err := quote.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("portfolio quote: %w", err)
	}
	if quote.Symbol != p.symbol {
		return Snapshot{}, fmt.Errorf("portfolio: expected quote symbol %q, got %q", p.symbol, quote.Symbol)
	}
	marketValue, err := domain.Notional(quote.Bid, p.position)
	if err != nil {
		return Snapshot{}, fmt.Errorf("portfolio market value: %w", err)
	}
	if marketValue > math.MaxInt64-p.cash {
		return Snapshot{}, fmt.Errorf("portfolio equity overflows int64")
	}
	equity := p.cash + marketValue
	// Both operands are nonnegative int64 values, so their difference is safe.
	return Snapshot{Cash: p.cash, Position: p.position, MarketValue: marketValue, Equity: equity, PnL: equity - p.initialCash}, nil
}
