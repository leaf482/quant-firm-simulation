package paper

import (
	"fmt"
	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"math"
	"strings"
	"testing"
	"time"
)

func inputs() (domain.Order, domain.Quote) {
	t := time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)
	return domain.Order{OrderID: "order-1", IntentID: "intent-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 3, Status: domain.OrderSubmitted, CreatedAt: t}, domain.Quote{Symbol: "AAPL", Timestamp: t.Add(time.Second), Bid: 1000000, Ask: 1010000}
}

func TestExecutionAndDeterministicIDs(t *testing.T) {
	var firstRun []domain.Fill
	for run := 0; run < 2; run++ {
		b := NewBroker()
		for n, side := range []domain.Side{domain.Buy, domain.Sell} {
			o, q := inputs()
			o.OrderID = domain.OrderID(fmt.Sprintf("order-%d", n+1))
			o.Side = side
			originalOrder, originalQuote := o, q
			f, err := b.Execute(o, q)
			if err != nil {
				t.Fatal(err)
			}
			price := q.Ask
			if side == domain.Sell {
				price = q.Bid
			}
			want := domain.Fill{FillID: domain.FillID(fmt.Sprintf("fill-%d", n+1)), OrderID: o.OrderID, Symbol: o.Symbol, Side: side, Quantity: o.Quantity, Price: price, Timestamp: q.Timestamp}
			if f != want {
				t.Fatalf("fill=%+v, want %+v", f, want)
			}
			if err := f.Validate(); err != nil {
				t.Fatal(err)
			}
			if o != originalOrder || q != originalQuote {
				t.Fatal("inputs mutated")
			}
			if run == 0 {
				firstRun = append(firstRun, f)
			} else if f != firstRun[n] {
				t.Fatal("non-deterministic fill")
			}
			// A retry must preserve the original price and simulated timestamp.
			q.Bid++
			q.Ask++
			q.Timestamp = q.Timestamp.Add(time.Second)
			o.CreatedAt = o.CreatedAt.In(time.FixedZone("offset", 3600))
			for retry := 0; retry < 2; retry++ {
				got, err := b.Execute(o, q)
				if err != nil || got != want {
					t.Fatalf("retry=%+v, %v", got, err)
				}
			}
			if len(b.executions) != n+1 || b.sequence != uint64(n+1) {
				t.Fatal("retry advanced sequence or fill count")
			}
			f.Quantity = 999
			got, err := b.Execute(o, q)
			if err != nil || got != want {
				t.Fatal("returned fill aliases stored state")
			}
		}
	}
}

func TestRejectedExecution(t *testing.T) {
	for _, tt := range []struct {
		name   string
		edit   func(*domain.Order, *domain.Quote)
		reason string
	}{
		{"new", func(o *domain.Order, q *domain.Quote) { o.Status = domain.OrderNew }, "SUBMITTED"},
		{"cancelled", func(o *domain.Order, q *domain.Quote) { o.Status = domain.OrderCancelled }, "SUBMITTED"},
		{"rejected", func(o *domain.Order, q *domain.Quote) { o.Status = domain.OrderRejected }, "SUBMITTED"},
		{"filled", func(o *domain.Order, q *domain.Quote) { o.Status = domain.OrderFilled }, "SUBMITTED"},
		{"symbol", func(o *domain.Order, q *domain.Quote) { q.Symbol = "MSFT" }, "symbol"},
		{"invalid order", func(o *domain.Order, q *domain.Quote) { o.Quantity = 0 }, "order"},
		{"invalid quote", func(o *domain.Order, q *domain.Quote) { q.Bid = q.Ask + 1 }, "quote"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBroker()
			o, q := inputs()
			tt.edit(&o, &q)
			if _, err := b.Execute(o, q); err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("error=%v", err)
			}
			if len(b.executions) != 0 || b.sequence != 0 {
				t.Fatal("rejection changed state")
			}
			o, q = inputs()
			f, err := b.Execute(o, q)
			if err != nil || f.FillID != "fill-1" {
				t.Fatalf("valid execution=%+v, %v", f, err)
			}
			tt.edit(&o, &q)
			if _, err := b.Execute(o, q); err == nil {
				t.Fatal("invalid retry accepted")
			}
			if len(b.executions) != 1 || b.sequence != 1 {
				t.Fatal("invalid retry changed state")
			}
		})
	}
}

func TestConflictingOrder(t *testing.T) {
	for _, edit := range []func(*domain.Order){
		func(o *domain.Order) { o.IntentID = "different" }, func(o *domain.Order) { o.Symbol = "MSFT" },
		func(o *domain.Order) { o.Side = domain.Sell }, func(o *domain.Order) { o.Quantity++ }, func(o *domain.Order) { o.CreatedAt = o.CreatedAt.Add(time.Second) },
	} {
		b := NewBroker()
		o, q := inputs()
		want, err := b.Execute(o, q)
		if err != nil {
			t.Fatal(err)
		}
		conflict := o
		edit(&conflict)
		changedQuote := q
		changedQuote.Symbol = conflict.Symbol
		if _, err := b.Execute(conflict, changedQuote); err == nil || !strings.Contains(err.Error(), "conflicting") {
			t.Fatalf("conflict error=%v", err)
		}
		got, err := b.Execute(o, q)
		if err != nil || got != want || b.sequence != 1 || len(b.executions) != 1 {
			t.Fatal("conflict mutated state")
		}
	}
}

func TestSequenceExhaustion(t *testing.T) {
	b := NewBroker()
	b.sequence = math.MaxUint64
	o, q := inputs()
	if _, err := b.Execute(o, q); err == nil {
		t.Fatal("sequence wrapped")
	}
	if len(b.executions) != 0 || b.sequence != math.MaxUint64 {
		t.Fatal("exhaustion changed state")
	}
}
