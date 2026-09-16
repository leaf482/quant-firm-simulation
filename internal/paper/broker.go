// Package paper simulates complete executions without modifying other components.
package paper

import (
	"fmt"
	"math"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

type execution struct {
	order domain.Order
	fill  domain.Fill
}

// Broker is single-threaded, in-memory, and returns fill copies. Its zero value
// is usable. Fill IDs are unique only within this broker instance's run.
type Broker struct {
	executions map[domain.OrderID]execution
	sequence   uint64
}

func NewBroker() *Broker { return &Broker{} }

// Execute fills a SUBMITTED order at the supplied quote: BUY at ask, SELL at bid.
// Every call requires a valid SUBMITTED order and matching valid quote, including
// retries. Identical retries return the original fill, even with a newer quote.
// Any non-SUBMITTED status, including FILLED, is rejected without a new fill.
func (b *Broker) Execute(order domain.Order, quote domain.Quote) (domain.Fill, error) {
	if err := order.Validate(); err != nil {
		return domain.Fill{}, fmt.Errorf("paper order: %w", err)
	}
	if err := quote.Validate(); err != nil {
		return domain.Fill{}, fmt.Errorf("paper quote: %w", err)
	}
	if order.Status != domain.OrderSubmitted {
		return domain.Fill{}, fmt.Errorf("paper: only SUBMITTED orders may execute, got %s", order.Status)
	}
	if order.Symbol != quote.Symbol {
		return domain.Fill{}, fmt.Errorf("paper: order symbol %q does not match quote symbol %q", order.Symbol, quote.Symbol)
	}
	if previous, ok := b.executions[order.OrderID]; ok {
		o := previous.order
		if o.IntentID != order.IntentID || o.Symbol != order.Symbol || o.Side != order.Side || o.Quantity != order.Quantity || !o.CreatedAt.Equal(order.CreatedAt) {
			return domain.Fill{}, fmt.Errorf("paper: conflicting payload for order %q", order.OrderID)
		}
		return previous.fill, nil
	}
	if b.sequence == math.MaxUint64 {
		return domain.Fill{}, fmt.Errorf("paper: fill ID sequence exhausted")
	}
	price := quote.Ask
	if order.Side == domain.Sell {
		price = quote.Bid
	}
	fill := domain.Fill{FillID: domain.FillID(fmt.Sprintf("fill-%d", b.sequence+1)), OrderID: order.OrderID, Symbol: order.Symbol, Side: order.Side, Quantity: order.Quantity, Price: price, Timestamp: quote.Timestamp}
	if err := fill.Validate(); err != nil {
		return domain.Fill{}, fmt.Errorf("paper fill: %w", err)
	}
	if b.executions == nil {
		b.executions = make(map[domain.OrderID]execution)
	}
	b.executions[order.OrderID] = execution{order: order, fill: fill}
	b.sequence++
	return fill, nil
}
