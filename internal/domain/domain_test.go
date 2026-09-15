package domain_test

import (
	"math"
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func TestQuantityValidation(t *testing.T) {
	for _, q := range []domain.Quantity{-1, 0, 1, 100, math.MaxInt64} {
		if err := q.Validate(); (err != nil) != (q <= 0) {
			t.Errorf("Quantity(%d).Validate() = %v", q, err)
		}
	}
}

func TestQuoteValidation(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	valid := domain.Quote{Symbol: "AAPL", Timestamp: stamp, Bid: 1000000, Ask: 1002500}
	for _, tt := range []struct {
		name    string
		edit    func(*domain.Quote)
		wantErr bool
	}{
		{"valid", func(q *domain.Quote) {}, false},
		{"equal prices", func(q *domain.Quote) { q.Ask = q.Bid }, false},
		{"empty symbol", func(q *domain.Quote) { q.Symbol = "" }, true},
		{"blank symbol", func(q *domain.Quote) { q.Symbol = " \t" }, true},
		{"zero timestamp", func(q *domain.Quote) { q.Timestamp = time.Time{} }, true},
		{"zero bid", func(q *domain.Quote) { q.Bid = 0 }, true},
		{"negative bid", func(q *domain.Quote) { q.Bid = -1 }, true},
		{"zero ask", func(q *domain.Quote) { q.Ask = 0 }, true},
		{"negative ask", func(q *domain.Quote) { q.Ask = -1 }, true},
		{"crossed", func(q *domain.Quote) { q.Bid = q.Ask + 1 }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := valid
			tt.edit(&q)
			if err := q.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOrderIntentValidation(t *testing.T) {
	valid := domain.OrderIntent{
		IntentID: "fixture-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1,
		Timestamp: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC),
	}
	for _, tt := range []struct {
		name    string
		edit    func(*domain.OrderIntent)
		wantErr bool
	}{
		{"buy", func(i *domain.OrderIntent) {}, false},
		{"sell", func(i *domain.OrderIntent) { i.Side = domain.Sell }, false},
		{"empty ID", func(i *domain.OrderIntent) { i.IntentID = "" }, true},
		{"blank ID", func(i *domain.OrderIntent) { i.IntentID = " \t" }, true},
		{"empty symbol", func(i *domain.OrderIntent) { i.Symbol = "" }, true},
		{"blank symbol", func(i *domain.OrderIntent) { i.Symbol = " " }, true},
		{"empty side", func(i *domain.OrderIntent) { i.Side = "" }, true},
		{"unknown side", func(i *domain.OrderIntent) { i.Side = "SHORT" }, true},
		{"lowercase side", func(i *domain.OrderIntent) { i.Side = "buy" }, true},
		{"zero quantity", func(i *domain.OrderIntent) { i.Quantity = 0 }, true},
		{"negative quantity", func(i *domain.OrderIntent) { i.Quantity = -1 }, true},
		{"zero timestamp", func(i *domain.OrderIntent) { i.Timestamp = time.Time{} }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			i := valid
			tt.edit(&i)
			if err := i.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
