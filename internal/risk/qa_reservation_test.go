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

func TestQAReservedRiskExtremeBoundaries(t *testing.T) {
	c, err := risk.NewChecker(risk.Limits{MaxOrderNotional: math.MaxInt64, MaxPosition: math.MaxInt64})
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name      string
		resources reservation.Resources
		side      domain.Side
		quantity  domain.Quantity
		price     domain.Price
		reason    string
	}{
		{"maximum price with active sell", reservation.Resources{Symbol: "XYZ", Cash: math.MaxInt64, Position: 1, ReservedSell: 1}, domain.Buy, 1, math.MaxInt64, ""},
		{"one money unit short", reservation.Resources{Symbol: "XYZ", Cash: math.MaxInt64, ReservedCash: 1, OutstandingBuy: 1}, domain.Buy, 1, math.MaxInt64, "insufficient cash"},
		{"maximum projected exact", reservation.Resources{Symbol: "XYZ", Cash: math.MaxInt64, ReservedCash: math.MaxInt64 - 1, OutstandingBuy: math.MaxInt64 - 1}, domain.Buy, 1, 1, ""},
		{"maximum projected plus one", reservation.Resources{Symbol: "XYZ", Cash: 2, ReservedCash: 1, OutstandingBuy: 1, Position: math.MaxInt64 - 1, ReservedSell: math.MaxInt64 - 1}, domain.Buy, 1, 1, "maximum position"},
		{"maximum sell exact", reservation.Resources{Symbol: "XYZ", Position: math.MaxInt64, ReservedSell: 1}, domain.Sell, math.MaxInt64 - 1, 1, ""},
		{"maximum sell oversell by one", reservation.Resources{Symbol: "XYZ", Position: math.MaxInt64, ReservedSell: 1}, domain.Sell, math.MaxInt64, 1, "insufficient holdings"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.resources
			intent := domain.OrderIntent{IntentID: "qa", Symbol: "XYZ", Side: tt.side, Quantity: tt.quantity, Timestamp: stamp}
			quote := domain.Quote{Symbol: "XYZ", Timestamp: stamp, Bid: tt.price, Ask: tt.price}
			for i := 0; i < 2; i++ {
				err := c.CheckReserved(intent, quote, tt.resources)
				var rejection *risk.Rejection
				if tt.reason == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.As(err, &rejection) || !strings.Contains(err.Error(), tt.reason) {
					t.Fatalf("error=%v want %q", err, tt.reason)
				}
			}
			if tt.resources != before {
				t.Fatal("risk mutated resources")
			}
		})
	}
}
