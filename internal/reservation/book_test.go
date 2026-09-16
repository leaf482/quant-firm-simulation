package reservation_test

import (
	"math"
	"testing"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/reservation"
)

func book(t *testing.T) *reservation.Book {
	t.Helper()
	b, err := reservation.NewBook("XYZ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func buy(id domain.OrderID, price domain.Price, quantity domain.Quantity) reservation.Terms {
	return reservation.Terms{OrderID: id, Symbol: "XYZ", Side: domain.Buy, Quantity: quantity, ReferencePrice: price, Cash: domain.Money(price) * domain.Money(quantity)}
}

func sell(id domain.OrderID, quantity domain.Quantity) reservation.Terms {
	return reservation.Terms{OrderID: id, Symbol: "XYZ", Side: domain.Sell, Quantity: quantity}
}

func snapshot(t *testing.T, b *reservation.Book, cash domain.Money, position domain.Quantity) reservation.Resources {
	t.Helper()
	r, err := b.Snapshot(cash, position)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCompetingResourcesAndExactBoundaries(t *testing.T) {
	b := book(t)
	// $1,000 actual cash, five actual shares; reference ask is $100.
	cash, position := domain.Money(10000000), domain.Quantity(5)
	if _, err := b.Acquire(buy("b1", 1000000, 6), cash, position); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(sell("s1", 4), cash, position); err != nil {
		t.Fatal(err)
	}
	want := reservation.Resources{Symbol: "XYZ", Cash: cash, Position: position, ReservedCash: 6000000, ReservedSell: 4, OutstandingBuy: 6}
	if got := snapshot(t, b, cash, position); got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	a, err := want.Derive()
	if err != nil || a != (reservation.Available{Cash: 4000000, SellQuantity: 1, ProjectedPosition: 11}) {
		t.Fatalf("available=%+v err=%v", a, err)
	}
	for _, terms := range []reservation.Terms{buy("b2", 1000000, 6), sell("s2", 3)} {
		if _, err := b.Acquire(terms, cash, position); err == nil {
			t.Fatal("accepted competing reservation")
		}
		if got := snapshot(t, b, cash, position); got != want {
			t.Fatal("failure mutated resources")
		}
		if _, err := b.Get(terms.OrderID); err == nil {
			t.Fatal("failed acquisition retained identity")
		}
	}
	if _, err := b.Acquire(buy("b2", 1000000, 4), cash, position); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(sell("s2", 1), cash, position); err != nil {
		t.Fatal(err)
	}
	r := snapshot(t, b, cash, position)
	a, err = r.Derive()
	if err != nil || a != (reservation.Available{ProjectedPosition: 15}) {
		t.Fatalf("boundary=%+v err=%v", a, err)
	}
	if cash != 10000000 || position != 5 || r.Cash != cash || r.Position != position {
		t.Fatal("actual balances changed")
	}
}

func TestIdentityHistory(t *testing.T) {
	for _, terms := range []reservation.Terms{buy("buy", 10, 2), sell("sell", 2)} {
		for _, state := range []reservation.State{reservation.Released, reservation.Settled} {
			t.Run(string(terms.Side)+"/"+string(state), func(t *testing.T) {
				b := book(t)
				first, err := b.Acquire(terms, 100, 5)
				if err != nil {
					t.Fatal(err)
				}
				before := snapshot(t, b, 100, 5)
				// Retry lookup precedes current resource validation.
				if got, err := b.Acquire(terms, 0, 0); err != nil || got != first {
					t.Fatalf("retry=%+v %v", got, err)
				}
				conflict := terms
				conflict.Quantity++
				if _, err := b.Acquire(conflict, 100, 5); err == nil {
					t.Fatal("accepted conflicting acquisition")
				}
				if got := snapshot(t, b, 100, 5); got != before {
					t.Fatal("duplicate changed totals")
				}
				outcome := reservation.Outcome{ID: "outcome-1", State: state, Reason: "fixture"}
				terminal, err := b.Release(terms.OrderID, outcome)
				if err != nil || terminal.State != state || terminal.Terms != terms || terminal.Outcome != outcome {
					t.Fatalf("terminal=%+v %v", terminal, err)
				}
				for i := 0; i < 2; i++ {
					if got, err := b.Release(terms.OrderID, outcome); err != nil || got != terminal {
						t.Fatal("release retry changed outcome")
					}
					if got, err := b.Acquire(terms, 0, 0); err != nil || got != terminal {
						t.Fatal("acquisition reactivated terminal reservation")
					}
				}
				for _, changed := range []reservation.Outcome{
					{ID: "different", State: state, Reason: "fixture"},
					{ID: outcome.ID, State: state, Reason: "different"},
					{ID: outcome.ID, State: reservation.Active, Reason: "fixture"},
					{ID: outcome.ID, State: map[reservation.State]reservation.State{reservation.Released: reservation.Settled, reservation.Settled: reservation.Released}[state], Reason: "fixture"},
				} {
					if _, err := b.Release(terms.OrderID, changed); err == nil {
						t.Fatal("accepted conflicting outcome")
					}
				}
				if _, err := b.Acquire(conflict, 100, 5); err == nil {
					t.Fatal("terminal identity conflict accepted")
				}
				if got, err := b.Get(terms.OrderID); err != nil || got != terminal {
					t.Fatal("history changed")
				}
				want := reservation.Resources{Symbol: "XYZ", Cash: 100, Position: 5}
				if got := snapshot(t, b, 100, 5); got != want {
					t.Fatalf("release totals=%+v", got)
				}
			})
		}
	}
}

func TestInvalidAcquisitionsDoNotMutate(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*reservation.Terms)
	}{
		{"empty ID", func(x *reservation.Terms) { x.OrderID = " " }},
		{"symbol", func(x *reservation.Terms) { x.Symbol = "OTHER" }},
		{"side", func(x *reservation.Terms) { x.Side = "bad" }},
		{"zero quantity", func(x *reservation.Terms) { x.Quantity = 0 }},
		{"negative quantity", func(x *reservation.Terms) { x.Quantity = -1 }},
		{"zero cash", func(x *reservation.Terms) { x.Cash = 0 }},
		{"negative cash", func(x *reservation.Terms) { x.Cash = -1 }},
		{"wrong cash", func(x *reservation.Terms) { x.Cash++ }},
		{"zero price", func(x *reservation.Terms) { x.ReferencePrice = 0 }},
		{"notional overflow", func(x *reservation.Terms) { x.ReferencePrice = math.MaxInt64 }},
		{"sell with cash", func(x *reservation.Terms) { x.Side = domain.Sell }},
		{"sell with price", func(x *reservation.Terms) { x.Side = domain.Sell; x.Cash = 0 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := book(t)
			terms := buy("bad", 10, 2)
			tt.edit(&terms)
			before := snapshot(t, b, 100, 5)
			if _, err := b.Acquire(terms, 100, 5); err == nil {
				t.Fatal("accepted invalid terms")
			}
			if got := snapshot(t, b, 100, 5); got != before {
				t.Fatal("failure mutated totals")
			}
			if _, err := b.Get(terms.OrderID); err == nil {
				t.Fatal("failure created entry")
			}
		})
	}
}

func TestAggregateOverflowAndInvalidBalances(t *testing.T) {
	for _, tt := range []struct {
		name          string
		first, second reservation.Terms
		cash          domain.Money
		position      domain.Quantity
	}{
		{"cash sum", buy("one", math.MaxInt64, 1), buy("two", 1, 1), math.MaxInt64, 0},
		{"buy quantity sum", buy("one", 1, math.MaxInt64), buy("two", 1, 1), math.MaxInt64, 0},
		{"sell sum", sell("one", math.MaxInt64), sell("two", 1), 0, math.MaxInt64},
		{"projected sum", sell("one", 1), buy("two", 1, 1), 1, math.MaxInt64},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := book(t)
			if _, err := b.Acquire(tt.first, tt.cash, tt.position); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, b, tt.cash, tt.position)
			if _, err := b.Acquire(tt.second, tt.cash, tt.position); err == nil {
				t.Fatal("accepted overflow")
			}
			if got := snapshot(t, b, tt.cash, tt.position); got != before {
				t.Fatal("overflow mutated resources")
			}
			if _, err := b.Get(tt.second.OrderID); err == nil {
				t.Fatal("overflow created entry")
			}
		})
	}
	b := book(t)
	if _, err := b.Acquire(buy("one", 10, 1), 10, 0); err != nil {
		t.Fatal(err)
	}
	for _, balances := range []struct {
		cash     domain.Money
		position domain.Quantity
	}{{-1, 0}, {10, -1}, {9, 0}} {
		if _, err := b.Snapshot(balances.cash, balances.position); err == nil {
			t.Fatal("accepted invalid balances")
		}
		if _, err := b.Acquire(sell("two", 1), balances.cash, balances.position); err == nil {
			t.Fatal("acquired on invalid snapshot")
		}
	}
	if got := snapshot(t, b, 10, 0); got.ReservedCash != 10 || got.OutstandingBuy != 1 {
		t.Fatal("invalid snapshot mutated book")
	}
}

func TestResourcesFailClosed(t *testing.T) {
	for _, r := range []reservation.Resources{
		{}, {Symbol: "XYZ", Cash: -1}, {Symbol: "XYZ", Position: -1},
		{Symbol: "XYZ", ReservedCash: -1}, {Symbol: "XYZ", ReservedSell: -1}, {Symbol: "XYZ", OutstandingBuy: -1},
		{Symbol: "XYZ", Cash: 1, ReservedCash: 2, OutstandingBuy: 1},
		{Symbol: "XYZ", Position: 1, ReservedSell: 2},
		{Symbol: "XYZ", Cash: 1, ReservedCash: 1}, {Symbol: "XYZ", OutstandingBuy: 1},
		{Symbol: "XYZ", Cash: 1, ReservedCash: 1, OutstandingBuy: 2},
		{Symbol: "XYZ", Cash: 1, ReservedCash: 1, OutstandingBuy: 1, Position: math.MaxInt64},
	} {
		if _, err := r.Derive(); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}

func TestInvalidReleaseAndConstruction(t *testing.T) {
	if _, err := reservation.NewBook(" "); err == nil {
		t.Fatal("accepted blank symbol")
	}
	var zero reservation.Book
	if _, err := zero.Acquire(buy("x", 1, 1), 1, 0); err == nil {
		t.Fatal("accepted zero book")
	}
	if _, err := zero.Snapshot(0, 0); err == nil {
		t.Fatal("accepted zero snapshot")
	}
	b := book(t)
	terms := buy("x", 1, 1)
	entry, err := b.Acquire(terms, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []reservation.Outcome{{}, {ID: "x", State: reservation.Active, Reason: "r"}, {ID: "x", State: reservation.Released}, {State: reservation.Settled, Reason: "r"}} {
		if _, err := b.Release("x", outcome); err == nil {
			t.Fatal("invalid release accepted")
		}
	}
	if _, err := b.Release("unknown", reservation.Outcome{ID: "x", State: reservation.Released, Reason: "r"}); err == nil {
		t.Fatal("unknown release accepted")
	}
	if got, err := b.Get("x"); err != nil || got != entry {
		t.Fatal("invalid release mutated entry")
	}
	if got := snapshot(t, b, 1, 0); got.ReservedCash != 1 {
		t.Fatal("invalid release changed totals")
	}
	// Returned values cannot mutate retained terms.
	entry.Terms.Quantity = 99
	if got, _ := b.Get("x"); got.Terms != terms {
		t.Fatal("entry aliases retained state")
	}
}
