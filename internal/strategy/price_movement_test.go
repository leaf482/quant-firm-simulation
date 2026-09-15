package strategy

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func quote(bid, ask domain.Price) domain.Quote {
	return domain.Quote{Symbol: "AAPL", Timestamp: time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC), Bid: bid, Ask: ask}
}

func TestPriceMovementSequence(t *testing.T) {
	steps := []struct {
		bid, ask domain.Price
		side     domain.Side
	}{
		{1000000, 1000200, ""},
		{1000200, 1000400, domain.Buy},
		{1000100, 1000300, domain.Sell},
		{1000000, 1000400, ""},
		{1000200, 1000400, domain.Buy},
		{999800, 1000000, domain.Sell},
	}
	firstRun := make([]domain.OrderIntent, 0)
	for run := 0; run < 2; run++ {
		s := NewPriceMovement()
		count := 0
		seen := make(map[domain.IntentID]bool)
		for i, step := range steps {
			q := quote(step.bid, step.ask)
			q.Timestamp = q.Timestamp.Add(time.Duration(i) * time.Second)
			intent, err := s.OnQuote(q)
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			if step.side == "" {
				if intent != nil {
					t.Fatalf("step %d: expected no signal, got %+v", i, intent)
				}
				continue
			}
			if intent == nil {
				t.Fatalf("step %d: missing intent", i)
			}
			if intent.Side != step.side || intent.Quantity != 1 || intent.Symbol != q.Symbol || !intent.Timestamp.Equal(q.Timestamp) {
				t.Fatalf("step %d: unexpected intent %+v", i, intent)
			}
			if err := intent.Validate(); err != nil {
				t.Fatal(err)
			}
			wantID := domain.IntentID(fmt.Sprintf("price-movement-%d", count+1))
			if intent.IntentID != wantID || seen[intent.IntentID] {
				t.Fatalf("unexpected or duplicate ID %q", intent.IntentID)
			}
			seen[intent.IntentID] = true
			if run == 0 {
				firstRun = append(firstRun, *intent)
			} else if *intent != firstRun[count] {
				t.Fatalf("nondeterministic intent: %+v", intent)
			}
			count++
		}
	}
}

func TestMidpointRoundingAndOverflow(t *testing.T) {
	for _, tt := range []struct {
		name          string
		first, second domain.Quote
		want          domain.Side
	}{
		{"half unit rounds down", quote(10000, 10000), quote(10000, 10001), ""},
		{"whole unit rises", quote(10000, 10001), quote(10001, 10002), domain.Buy},
		{"large prices", quote(math.MaxInt64-2, math.MaxInt64), quote(math.MaxInt64, math.MaxInt64), domain.Buy},
		{"wide spread", quote(1, 1), quote(1, math.MaxInt64), domain.Buy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := NewPriceMovement()
			if intent, err := s.OnQuote(tt.first); err != nil || intent != nil {
				t.Fatalf("first = %+v, %v", intent, err)
			}
			intent, err := s.OnQuote(tt.second)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if intent != nil {
					t.Fatalf("unexpected intent %+v", intent)
				}
				return
			}
			if intent == nil || intent.Side != tt.want {
				t.Fatalf("intent = %+v, want %s", intent, tt.want)
			}
		})
	}
}

func TestInvalidInputPreservesState(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		s := NewPriceMovement()
		if initialized {
			if _, err := s.OnQuote(quote(10000, 10000)); err != nil {
				t.Fatal(err)
			}
		}
		before := *s
		bad := quote(20000, 10000)
		if intent, err := s.OnQuote(bad); err == nil || intent != nil {
			t.Fatalf("invalid quote = %+v, %v", intent, err)
		}
		if *s != before {
			t.Fatal("invalid quote changed state")
		}
	}
	s := NewPriceMovement()
	if _, err := s.OnQuote(quote(10000, 10000)); err != nil {
		t.Fatal(err)
	}
	before := *s
	other := quote(20000, 20000)
	other.Symbol = "MSFT"
	if intent, err := s.OnQuote(other); err == nil || intent != nil {
		t.Fatalf("symbol change = %+v, %v", intent, err)
	}
	if *s != before {
		t.Fatal("symbol change changed state")
	}
}

func TestSequenceExhaustion(t *testing.T) {
	s := NewPriceMovement()
	if _, err := s.OnQuote(quote(10000, 10000)); err != nil {
		t.Fatal(err)
	}
	s.sequence = math.MaxUint64
	before := *s
	if intent, err := s.OnQuote(quote(20000, 20000)); err == nil || intent != nil {
		t.Fatalf("exhaustion = %+v, %v", intent, err)
	}
	if *s != before {
		t.Fatal("sequence exhaustion changed state")
	}
}
