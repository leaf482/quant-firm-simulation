// Package reservation tracks in-memory resources for one symbol. A Book has a
// single owner and performs no order allocation, accounting, execution or I/O.
package reservation

import (
	"fmt"
	"math"
	"strings"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

type State string

const (
	Active   State = "ACTIVE"
	Released State = "RELEASED"
	Settled  State = "SETTLED"
)

// Terms are immutable. BUY Cash must equal ReferencePrice * Quantity. SELL
// reserves Quantity and requires zero Cash and ReferencePrice. Money uses $0.0001.
type Terms struct {
	OrderID        domain.OrderID
	Symbol         domain.Symbol
	Side           domain.Side
	Quantity       domain.Quantity
	Cash           domain.Money
	ReferencePrice domain.Price
}

// Outcome retains the full release identity. Settled means resources were
// released for a caller-reported settlement; this package does not apply a fill.
// ID is the caller's stable release or settlement ID. Reason is immutable.
type Outcome struct {
	ID     string
	State  State
	Reason string
}

type Entry struct {
	Terms   Terms
	State   State
	Outcome Outcome
}

// Resources is a value snapshot supplied to risk. Actual balances are caller
// supplied; the book neither owns nor modifies the portfolio.
type Resources struct {
	Symbol         domain.Symbol
	Cash           domain.Money
	Position       domain.Quantity
	ReservedCash   domain.Money
	ReservedSell   domain.Quantity
	OutstandingBuy domain.Quantity
}

type Available struct {
	Cash              domain.Money
	SellQuantity      domain.Quantity
	ProjectedPosition domain.Quantity
}

// Derive rejects inconsistent aggregates rather than clamping them.
func (r Resources) Derive() (Available, error) {
	if strings.TrimSpace(string(r.Symbol)) == "" || r.Cash < 0 || r.Position < 0 ||
		r.ReservedCash < 0 || r.ReservedSell < 0 || r.OutstandingBuy < 0 {
		return Available{}, fmt.Errorf("reservation: invalid resource snapshot")
	}
	if r.ReservedCash > r.Cash || r.ReservedSell > r.Position {
		return Available{}, fmt.Errorf("reservation: resources exceed actual balances")
	}
	// Each reserved BUY share costs at least one fixed-point money unit.
	if (r.ReservedCash == 0) != (r.OutstandingBuy == 0) || r.ReservedCash < domain.Money(r.OutstandingBuy) {
		return Available{}, fmt.Errorf("reservation: inconsistent BUY aggregates")
	}
	if r.OutstandingBuy > math.MaxInt64-r.Position {
		return Available{}, fmt.Errorf("reservation: projected position overflows int64")
	}
	return Available{r.Cash - r.ReservedCash, r.Position - r.ReservedSell, r.Position + r.OutstandingBuy}, nil
}

type Book struct {
	symbol         domain.Symbol
	entries        map[domain.OrderID]Entry
	reservedCash   domain.Money
	reservedSell   domain.Quantity
	outstandingBuy domain.Quantity
}

func NewBook(symbol domain.Symbol) (*Book, error) {
	if strings.TrimSpace(string(symbol)) == "" {
		return nil, fmt.Errorf("reservation: blank symbol")
	}
	return &Book{symbol: symbol, entries: make(map[domain.OrderID]Entry)}, nil
}

// Get returns a copy, including terminal history.
func (b *Book) Get(id domain.OrderID) (Entry, error) {
	e, ok := b.entries[id]
	if !ok {
		return Entry{}, fmt.Errorf("reservation: unknown order %q", id)
	}
	return e, nil
}

func (b *Book) Snapshot(cash domain.Money, position domain.Quantity) (Resources, error) {
	r := Resources{b.symbol, cash, position, b.reservedCash, b.reservedSell, b.outstandingBuy}
	if _, err := r.Derive(); err != nil {
		return Resources{}, err
	}
	return r, nil
}

// Acquire checks retries before current balances: matching historical terms
// return their original entry, even after release, without reactivation.
// New acquisitions validate the complete prospective state before mutation.
func (b *Book) Acquire(t Terms, cash domain.Money, position domain.Quantity) (Entry, error) {
	if old, ok := b.entries[t.OrderID]; ok {
		if old.Terms != t {
			return Entry{}, fmt.Errorf("reservation: conflicting terms for %q", t.OrderID)
		}
		return old, nil
	}
	if b.entries == nil || strings.TrimSpace(string(t.OrderID)) == "" || t.Symbol != b.symbol {
		return Entry{}, fmt.Errorf("reservation: invalid identity or uninitialized book")
	}
	if err := t.Quantity.Validate(); err != nil {
		return Entry{}, err
	}
	r, err := b.Snapshot(cash, position)
	if err != nil {
		return Entry{}, err
	}
	switch t.Side {
	case domain.Buy:
		value, err := domain.Notional(t.ReferencePrice, t.Quantity)
		if err != nil {
			return Entry{}, err
		}
		if t.Cash <= 0 || t.Cash != value {
			return Entry{}, fmt.Errorf("reservation: invalid BUY cash")
		}
		if t.Cash > math.MaxInt64-r.ReservedCash || t.Quantity > math.MaxInt64-r.OutstandingBuy {
			return Entry{}, fmt.Errorf("reservation: BUY aggregate overflows int64")
		}
		r.ReservedCash += t.Cash
		r.OutstandingBuy += t.Quantity
	case domain.Sell:
		if t.Cash != 0 || t.ReferencePrice != 0 {
			return Entry{}, fmt.Errorf("reservation: SELL must reserve only quantity")
		}
		if t.Quantity > math.MaxInt64-r.ReservedSell {
			return Entry{}, fmt.Errorf("reservation: SELL aggregate overflows int64")
		}
		r.ReservedSell += t.Quantity
	default:
		return Entry{}, fmt.Errorf("reservation: invalid side")
	}
	if _, err := r.Derive(); err != nil {
		return Entry{}, err
	}
	e := Entry{Terms: t, State: Active}
	b.entries[t.OrderID] = e
	b.reservedCash, b.reservedSell, b.outstandingBuy = r.ReservedCash, r.ReservedSell, r.OutstandingBuy
	return e, nil
}

// Release retains a terminal outcome. Matching retries are no-ops; different
// identities, reasons or terminal states conflict. Released and Settled cannot
// transition to each other. It never changes portfolio balances.
func (b *Book) Release(id domain.OrderID, outcome Outcome) (Entry, error) {
	e, err := b.Get(id)
	if err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(outcome.ID) == "" || strings.TrimSpace(outcome.Reason) == "" ||
		(outcome.State != Released && outcome.State != Settled) {
		return Entry{}, fmt.Errorf("reservation: invalid release outcome")
	}
	if e.State != Active {
		if e.Outcome != outcome {
			return Entry{}, fmt.Errorf("reservation: conflicting outcome for %q", id)
		}
		return e, nil
	}
	if e.Terms.Side == domain.Buy {
		b.reservedCash -= e.Terms.Cash
		b.outstandingBuy -= e.Terms.Quantity
	} else {
		b.reservedSell -= e.Terms.Quantity
	}
	e.State, e.Outcome = outcome.State, outcome
	b.entries[id] = e
	return e, nil
}
