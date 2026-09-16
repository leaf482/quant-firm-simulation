package oms

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func testIntent() domain.OrderIntent {
	return domain.OrderIntent{IntentID: "intent-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 2, Timestamp: time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)}
}

func TestCreateAndDeduplicate(t *testing.T) {
	for run := 0; run < 2; run++ {
		m := NewManager()
		for n := 1; n <= 2; n++ {
			i := testIntent()
			i.IntentID = domain.IntentID(fmt.Sprintf("intent-%d", n))
			o, err := m.Create(i)
			if err != nil {
				t.Fatal(err)
			}
			want := domain.Order{OrderID: domain.OrderID(fmt.Sprintf("order-%d", n)), IntentID: i.IntentID, Symbol: i.Symbol, Side: i.Side, Quantity: i.Quantity, Status: domain.OrderNew, CreatedAt: i.Timestamp}
			if o != want {
				t.Fatalf("got %+v, want %+v", o, want)
			}
			if err := o.Validate(); err != nil {
				t.Fatal(err)
			}
			for repeat := 0; repeat < 3; repeat++ {
				got, err := m.Create(i)
				if err != nil || got != want {
					t.Fatalf("duplicate = %+v, %v", got, err)
				}
			}
			if len(m.orders) != n || len(m.intents) != n {
				t.Fatal("duplicate increased order count")
			}
			o.Quantity = 999
			got, err := m.Create(i)
			if err != nil || got != want {
				t.Fatal("returned order aliases managed state")
			}
		}
	}
}

func TestTransitions(t *testing.T) {
	states := []domain.OrderStatus{domain.OrderNew, domain.OrderSubmitted, domain.OrderCancelled, domain.OrderRejected, domain.OrderFilled}
	for _, from := range states {
		for _, to := range append(states, "INVALID", "") {
			t.Run(string(from)+"-"+string(to), func(t *testing.T) {
				m := NewManager()
				i := testIntent()
				o, err := m.Create(i)
				if err != nil {
					t.Fatal(err)
				}
				if from == domain.OrderSubmitted || from == domain.OrderCancelled || from == domain.OrderFilled {
					if _, err = m.Transition(o.OrderID, domain.OrderSubmitted); err != nil {
						t.Fatal(err)
					}
				}
				if from == domain.OrderCancelled || from == domain.OrderRejected || from == domain.OrderFilled {
					if _, err = m.Transition(o.OrderID, from); err != nil {
						t.Fatal(err)
					}
				}
				allowed := from == to || (from == domain.OrderNew && (to == domain.OrderSubmitted || to == domain.OrderRejected)) || (from == domain.OrderSubmitted && (to == domain.OrderCancelled || to == domain.OrderRejected || to == domain.OrderFilled))
				got, err := m.Transition(o.OrderID, to)
				if (err == nil) != allowed {
					t.Fatalf("transition error=%v, allowed=%v", err, allowed)
				}
				wantStatus := from
				if allowed {
					wantStatus = to
					if got.Status != to {
						t.Fatal("incorrect returned status")
					}
				}
				current, err := m.Create(i)
				if err != nil || current.Status != wantStatus || current.OrderID != o.OrderID || len(m.orders) != 1 {
					t.Fatalf("duplicate after transition = %+v, %v", current, err)
				}
			})
		}
	}
}

func TestInvalidIntentAndUnknownOrder(t *testing.T) {
	m := NewManager()
	bad := testIntent()
	bad.Quantity = 0
	if _, err := m.Create(bad); err == nil {
		t.Fatal("invalid intent accepted")
	}
	if len(m.orders) != 0 || len(m.intents) != 0 || m.sequence != 0 {
		t.Fatal("invalid intent changed state")
	}
	if _, err := m.Transition("missing", domain.OrderSubmitted); err == nil {
		t.Fatal("unknown ID accepted")
	}
	o, err := m.Create(testIntent())
	if err != nil || o.OrderID != "order-1" {
		t.Fatalf("create=%+v, %v", o, err)
	}
}

func TestConflictingIntent(t *testing.T) {
	for _, edit := range []func(*domain.OrderIntent){
		func(i *domain.OrderIntent) { i.Symbol = "MSFT" }, func(i *domain.OrderIntent) { i.Side = domain.Sell },
		func(i *domain.OrderIntent) { i.Quantity++ }, func(i *domain.OrderIntent) { i.Timestamp = i.Timestamp.Add(time.Second) },
	} {
		m := NewManager()
		i := testIntent()
		original, err := m.Create(i)
		if err != nil {
			t.Fatal(err)
		}
		conflict := i
		edit(&conflict)
		if _, err := m.Create(conflict); err == nil || !strings.Contains(err.Error(), "conflicting") {
			t.Fatalf("conflict error=%v", err)
		}
		got, err := m.Create(i)
		if err != nil || got != original || len(m.orders) != 1 || m.sequence != 1 {
			t.Fatal("conflict changed state")
		}
		i.Timestamp = i.Timestamp.In(time.FixedZone("offset", 3600))
		got, err = m.Create(i)
		if err != nil || got != original {
			t.Fatal("equivalent timestamp was not deduplicated")
		}
	}
}

func TestSequenceExhaustion(t *testing.T) {
	m := NewManager()
	m.sequence = math.MaxUint64
	if _, err := m.Create(testIntent()); err == nil {
		t.Fatal("sequence wrapped")
	}
	if len(m.orders) != 0 || len(m.intents) != 0 || m.sequence != math.MaxUint64 {
		t.Fatal("exhaustion changed state")
	}
}
