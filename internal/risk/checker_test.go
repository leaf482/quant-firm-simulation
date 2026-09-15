package risk_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/risk"
)

func TestLimits(t *testing.T) {
	for _, limits := range []risk.Limits{{0, 1}, {-1, 1}, {1, 0}, {1, -1}} {
		if c, err := risk.NewChecker(limits); err == nil || c != nil {
			t.Fatalf("accepted invalid limits %+v", limits)
		}
	}
	if _, err := risk.NewChecker(risk.Limits{MaxOrderNotional: 1, MaxPosition: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestCheck(t *testing.T) {
	stamp := time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)
	type input struct {
		intent   domain.OrderIntent
		quote    domain.Quote
		cash     domain.Price
		position domain.Quantity
		limits   risk.Limits
	}
	for _, tt := range []struct {
		name   string
		edit   func(*input)
		reason string
	}{
		{"buy approved", func(*input) {}, ""},
		{"cash exact", func(x *input) { x.cash = 2020000 }, ""},
		{"cash below ask cost", func(x *input) { x.cash = 2019999 }, "insufficient cash"},
		{"notional exact", func(x *input) { x.limits.MaxOrderNotional = 2020000 }, ""},
		{"notional below ask cost", func(x *input) { x.limits.MaxOrderNotional = 2019999 }, "maximum order notional"},
		{"position exact", func(x *input) { x.position = 8 }, ""},
		{"position exceeded", func(x *input) { x.position = 9 }, "maximum position"},
		{"position addition overflow", func(x *input) { x.position = math.MaxInt64; x.limits.MaxPosition = math.MaxInt64 }, "maximum position"},
		{"sell approved", func(x *input) { x.intent.Side = domain.Sell }, ""},
		{"sell exact holdings", func(x *input) { x.intent.Side = domain.Sell; x.position = 2 }, ""},
		{"sell insufficient", func(x *input) { x.intent.Side = domain.Sell; x.position = 1 }, "insufficient holdings"},
		{"sell zero holdings", func(x *input) { x.intent.Side = domain.Sell; x.position = 0 }, "insufficient holdings"},
		{"sell can reduce above limits", func(x *input) {
			x.intent.Side = domain.Sell
			x.cash = 0
			x.position = 20
			x.limits.MaxOrderNotional = 1
		}, ""},
		{"symbol mismatch", func(x *input) { x.intent.Symbol = "MSFT" }, "symbol"},
		{"invalid intent ID", func(x *input) { x.intent.IntentID = "" }, "intent"},
		{"invalid quantity", func(x *input) { x.intent.Quantity = 0 }, "intent"},
		{"invalid side", func(x *input) { x.intent.Side = "OTHER" }, "intent"},
		{"invalid quote", func(x *input) { x.quote.Bid = x.quote.Ask + 1 }, "quote"},
		{"negative cash", func(x *input) { x.cash = -1 }, "cash must not be negative"},
		{"negative holdings", func(x *input) { x.position = -1 }, "position must not be negative"},
		{"buy overflow", func(x *input) { x.quote.Ask = math.MaxInt64 }, "overflows"},
		{"sell overflow", func(x *input) { x.intent.Side = domain.Sell; x.quote.Bid = math.MaxInt64; x.quote.Ask = math.MaxInt64 }, "overflows"},
		{"sell uses bid", func(x *input) { x.intent.Side = domain.Sell; x.quote.Ask = math.MaxInt64 }, ""},
		{"maximum notional exact", func(x *input) {
			x.intent.Quantity = 1
			x.quote.Ask = math.MaxInt64
			x.cash = math.MaxInt64
			x.limits.MaxOrderNotional = math.MaxInt64
		}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			x := input{intent: domain.OrderIntent{IntentID: "test-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 2, Timestamp: stamp}, quote: domain.Quote{Symbol: "AAPL", Timestamp: stamp, Bid: 1000000, Ask: 1010000}, cash: 10000000, position: 5, limits: risk.Limits{MaxOrderNotional: 5000000, MaxPosition: 10}}
			tt.edit(&x)
			c, err := risk.NewChecker(x.limits)
			if err != nil {
				t.Fatal(err)
			}
			before := x
			for repeat := 0; repeat < 2; repeat++ {
				err = c.Check(x.intent, x.quote, x.cash, x.position)
				if tt.reason == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tt.reason) {
					t.Fatalf("error=%v, want %q", err, tt.reason)
				}
			}
			if x != before {
				t.Fatal("check mutated input")
			}
		})
	}
}

func TestZeroCheckerRejects(t *testing.T) {
	var c risk.Checker
	if err := c.Check(domain.OrderIntent{}, domain.Quote{}, 0, 0); err == nil {
		t.Fatal("zero checker approved")
	}
}
