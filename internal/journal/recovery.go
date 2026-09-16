package journal

import (
	"fmt"
	"io"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/oms"
	"github.com/leaf482/quant-firm-simulation/internal/paper"
	"github.com/leaf482/quant-firm-simulation/internal/portfolio"
)

type State struct {
	Orders    *oms.Manager
	Portfolio *portfolio.Portfolio
	Broker    *paper.Broker
	Records   int
}

// Recover always builds fresh components and returns no state on any error.
// A complete prefix may end at NEW, SUBMITTED, or fill-applied-before-FILLED.
// Recovery preserves that exact boundary; it does not invent missing transitions.
func Recover(input io.Reader) (*State, error) {
	records, err := Read(input)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("journal has no committed trading records")
	}
	first := records[0]
	account, err := portfolio.New(first.Symbol, first.InitialCash)
	if err != nil {
		return nil, err
	}
	state := &State{Orders: oms.NewManager(), Portfolio: account, Broker: paper.NewBroker()}
	filled := make(map[domain.OrderID]bool)
	for _, r := range records {
		if r.Symbol != first.Symbol || r.InitialCash != first.InitialCash {
			return nil, fmt.Errorf("journal sequence %d: account configuration changed", r.Sequence)
		}
		if err := state.apply(r.Event, first.Symbol, filled); err != nil {
			return nil, fmt.Errorf("journal sequence %d: %w", r.Sequence, err)
		}
		state.Records++
	}
	return state, nil
}

func (s *State) apply(e Event, symbol domain.Symbol, filled map[domain.OrderID]bool) error {
	switch e.Type {
	case OrderCreated:
		o := *e.Order
		if o.Symbol != symbol {
			return fmt.Errorf("order symbol mismatch")
		}
		if _, err := s.Orders.Get(o.OrderID); err == nil {
			return fmt.Errorf("duplicate order creation %q", o.OrderID)
		}
		expected := domain.OrderID(fmt.Sprintf("order-%d", len(s.Orders.Orders())+1))
		if o.OrderID != expected {
			return fmt.Errorf("unexpected order ID %q", o.OrderID)
		}
		got, err := s.Orders.Create(domain.OrderIntent{IntentID: o.IntentID, Symbol: o.Symbol, Side: o.Side, Quantity: o.Quantity, Timestamp: o.CreatedAt})
		if err != nil {
			return err
		}
		if got.OrderID != o.OrderID {
			return fmt.Errorf("intent reused for another order")
		}
	case OrderChanged:
		c := e.Change
		if c.Status == domain.OrderFilled && !filled[c.OrderID] {
			return fmt.Errorf("FILLED requires an applied fill")
		}
		if filled[c.OrderID] && c.Status != domain.OrderFilled {
			return fmt.Errorf("cannot change filled execution to %s", c.Status)
		}
		_, err := s.Orders.Transition(c.OrderID, c.Status)
		return err
	case FillApplied:
		f := *e.Fill
		o, err := s.Orders.Get(f.OrderID)
		if err != nil {
			return err
		}
		if o.Status != domain.OrderSubmitted && !(o.Status == domain.OrderFilled && filled[o.OrderID]) {
			return fmt.Errorf("fill requires SUBMITTED order")
		}
		o.Status = domain.OrderSubmitted // A matching retry may follow the FILLED record.
		if err := s.Broker.Restore(o, f); err != nil {
			return err
		}
		if err := s.Portfolio.Apply(f); err != nil {
			return err
		}
		filled[o.OrderID] = true
	}
	return nil
}
