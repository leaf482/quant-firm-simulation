package reservation_test

import (
	"testing"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/reservation"
)

func TestQAMixedHistoryAndResourceReuse(t *testing.T) {
	b := book(t)
	terms := []reservation.Terms{buy("b1", 7, 3), buy("b2", 11, 4), sell("s1", 2), sell("s2", 5)}
	for _, x := range terms {
		if _, err := b.Acquire(x, 100, 10); err != nil {
			t.Fatal(err)
		}
	}
	check := func(cash domain.Money, sells, buys domain.Quantity, available reservation.Available) {
		t.Helper()
		r := snapshot(t, b, 100, 10)
		want := reservation.Resources{Symbol: "XYZ", Cash: 100, Position: 10, ReservedCash: cash, ReservedSell: sells, OutstandingBuy: buys}
		if r != want {
			t.Fatalf("resources=%+v want %+v", r, want)
		}
		if got, err := r.Derive(); err != nil || got != available {
			t.Fatalf("available=%+v err=%v want %+v", got, err, available)
		}
	}
	check(65, 7, 7, reservation.Available{Cash: 35, SellQuantity: 3, ProjectedPosition: 17})
	for _, step := range []struct {
		index int
		state reservation.State
	}{{0, reservation.Released}, {2, reservation.Settled}} {
		x := terms[step.index]
		outcome := reservation.Outcome{ID: string(x.OrderID) + "-done", State: step.state, Reason: "QA"}
		terminal, err := b.Release(x.OrderID, outcome)
		if err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, b, 100, 10)
		for i := 0; i < 3; i++ {
			if got, err := b.Release(x.OrderID, outcome); err != nil || got != terminal {
				t.Fatalf("terminal retry=%+v %v", got, err)
			}
			if got, err := b.Acquire(x, 0, 0); err != nil || got != terminal {
				t.Fatalf("historical acquisition=%+v %v", got, err)
			}
		}
		if snapshot(t, b, 100, 10) != before {
			t.Fatal("terminal retries changed other reservations")
		}
	}
	check(44, 5, 4, reservation.Available{Cash: 56, SellQuantity: 5, ProjectedPosition: 14})
	// A failed acquisition must leave its ID reusable once resources are freed.
	replacement := buy("replacement", 1, 57)
	if _, err := b.Acquire(replacement, 100, 10); err == nil {
		t.Fatal("accepted cash overrun")
	}
	check(44, 5, 4, reservation.Available{Cash: 56, SellQuantity: 5, ProjectedPosition: 14})
	if _, err := b.Release("b2", reservation.Outcome{ID: "b2-done", State: reservation.Settled, Reason: "QA"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(replacement, 100, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(sell("s3", 5), 100, 10); err != nil {
		t.Fatal(err)
	}
	check(57, 10, 57, reservation.Available{Cash: 43, ProjectedPosition: 67})
}

func TestQAConflictingValidTermsPreserveActiveAndTerminalEntries(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		b := book(t)
		original := buy("id", 10, 2)
		entry, err := b.Acquire(original, 100, 10)
		if err != nil {
			t.Fatal(err)
		}
		if terminal {
			entry, err = b.Release("id", reservation.Outcome{ID: "done", State: reservation.Released, Reason: "QA"})
			if err != nil {
				t.Fatal(err)
			}
		}
		before := snapshot(t, b, 100, 10)
		for _, changed := range []reservation.Terms{
			buy("id", 10, 3), buy("id", 20, 2), buy("id", 5, 4), sell("id", 2),
			{OrderID: "id", Symbol: "OTHER", Side: domain.Buy, Quantity: 2, ReferencePrice: 10, Cash: 20},
		} {
			if _, err := b.Acquire(changed, 100, 10); err == nil {
				t.Fatalf("accepted conflict %+v", changed)
			}
			if got, err := b.Get("id"); err != nil || got != entry {
				t.Fatalf("entry changed: %+v %v", got, err)
			}
			if snapshot(t, b, 100, 10) != before {
				t.Fatal("conflict changed aggregates")
			}
		}
	}
}
