package risk_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"github.com/leaf482/quant-firm-simulation/internal/reservation"
	"github.com/leaf482/quant-firm-simulation/internal/risk"
)

func TestReservationAwareRisk(t *testing.T) {
	stamp := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name                  string
		cash                  domain.Money
		position              domain.Quantity
		buy, sell             domain.Quantity
		side                  domain.Side
		quantity, maxPosition domain.Quantity
		maxNotional           domain.Money
		reason                string
	}{
		{"competing buys", 10000000, 0, 6, 0, domain.Buy, 6, 100, 10000000, "insufficient cash"},
		{"competing sells", 0, 5, 0, 4, domain.Sell, 3, 100, 10000000, "insufficient holdings"},
		{"unfilled buys not sellable", 10000000, 0, 6, 0, domain.Sell, 1, 100, 10000000, "insufficient holdings"},
		{"sells do not free position", 10000000, 5, 0, 4, domain.Buy, 1, 5, 10000000, "maximum position"},
		{"sells do not supply cash", 0, 5, 0, 4, domain.Buy, 1, 100, 10000000, "insufficient cash"},
		{"buys count toward limit", 10000000, 2, 6, 0, domain.Buy, 3, 10, 10000000, "maximum position"},
		{"exact cash", 10000000, 0, 6, 0, domain.Buy, 4, 100, 10000000, ""},
		{"exact holdings", 0, 5, 0, 4, domain.Sell, 1, 100, 10000000, ""},
		{"exact projected limit", 10000000, 2, 6, 0, domain.Buy, 2, 10, 10000000, ""},
		{"notional limit", 10000000, 0, 0, 0, domain.Buy, 2, 100, 1999999, "maximum order notional"},
		{"exact notional", 10000000, 0, 0, 0, domain.Buy, 2, 100, 2000000, ""},
		{"sell above buy limits", 0, 5, 0, 1, domain.Sell, 4, 1, 1, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := reservation.NewBook("XYZ")
			if err != nil {
				t.Fatal(err)
			}
			var entries []reservation.Entry
			for _, terms := range []reservation.Terms{
				{OrderID: "buy", Symbol: "XYZ", Side: domain.Buy, Quantity: tt.buy, Cash: domain.Money(tt.buy) * 1000000, ReferencePrice: 1000000},
				{OrderID: "sell", Symbol: "XYZ", Side: domain.Sell, Quantity: tt.sell},
			} {
				if terms.Quantity == 0 {
					continue
				}
				e, err := b.Acquire(terms, tt.cash, tt.position)
				if err != nil {
					t.Fatal(err)
				}
				entries = append(entries, e)
			}
			r, err := b.Snapshot(tt.cash, tt.position)
			if err != nil {
				t.Fatal(err)
			}
			checker, err := risk.NewChecker(risk.Limits{MaxOrderNotional: tt.maxNotional, MaxPosition: tt.maxPosition})
			if err != nil {
				t.Fatal(err)
			}
			intent := domain.OrderIntent{IntentID: "new", Symbol: "XYZ", Side: tt.side, Quantity: tt.quantity, Timestamp: stamp}
			quote := domain.Quote{Symbol: "XYZ", Timestamp: stamp, Bid: 990000, Ask: 1000000}
			for repeat := 0; repeat < 2; repeat++ {
				checkErr := checker.CheckReserved(intent, quote, r)
				var rejection *risk.Rejection
				if tt.reason == "" {
					if checkErr != nil {
						t.Fatal(checkErr)
					}
				} else if !errors.As(checkErr, &rejection) || !strings.Contains(checkErr.Error(), tt.reason) {
					t.Fatalf("error=%v want rejection %q", checkErr, tt.reason)
				}
			}
			if after, err := b.Snapshot(tt.cash, tt.position); err != nil || after != r {
				t.Fatal("risk changed resource state")
			}
			for _, e := range entries {
				if got, err := b.Get(e.Terms.OrderID); err != nil || got != e {
					t.Fatal("risk changed reservation identity")
				}
			}
		})
	}
}

func TestReservedRiskInvalidStateIsFatal(t *testing.T) {
	stamp := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	c, err := risk.NewChecker(risk.Limits{MaxOrderNotional: math.MaxInt64, MaxPosition: math.MaxInt64})
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.OrderIntent{IntentID: "x", Symbol: "XYZ", Side: domain.Buy, Quantity: 2, Timestamp: stamp}
	quote := domain.Quote{Symbol: "XYZ", Timestamp: stamp, Bid: 1, Ask: 1}
	for _, r := range []reservation.Resources{
		{Symbol: "OTHER", Cash: 10},
		{Symbol: "XYZ", Cash: 1, ReservedCash: 2, OutstandingBuy: 1},
		{Symbol: "XYZ", Position: 1, ReservedSell: 2},
		{Symbol: "XYZ", Cash: 1, ReservedCash: 1, OutstandingBuy: 1, Position: math.MaxInt64},
	} {
		err := c.CheckReserved(intent, quote, r)
		var rejection *risk.Rejection
		if err == nil || errors.As(err, &rejection) {
			t.Fatalf("invalid state must be fatal, got %v", err)
		}
	}
	quote.Ask = math.MaxInt64
	err = c.CheckReserved(intent, quote, reservation.Resources{Symbol: "XYZ", Cash: math.MaxInt64})
	var rejection *risk.Rejection
	if err == nil || errors.As(err, &rejection) {
		t.Fatalf("notional overflow must be fatal, got %v", err)
	}
}
