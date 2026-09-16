package domain_test

import (
	"github.com/leaf482/quant-firm-simulation/internal/domain"
	"testing"
	"time"
)

func TestFillValidation(t *testing.T) {
	valid := domain.Fill{FillID: "fill-1", OrderID: "order-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 2, Price: 1000000, Timestamp: time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)}
	for _, side := range []domain.Side{domain.Buy, domain.Sell} {
		f := valid
		f.Side = side
		if err := f.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name string
		edit func(*domain.Fill)
	}{
		{"empty ID", func(f *domain.Fill) { f.FillID = "" }}, {"blank ID", func(f *domain.Fill) { f.FillID = " \t" }},
		{"order ID", func(f *domain.Fill) { f.OrderID = " " }}, {"symbol", func(f *domain.Fill) { f.Symbol = " " }},
		{"side", func(f *domain.Fill) { f.Side = "OTHER" }}, {"zero quantity", func(f *domain.Fill) { f.Quantity = 0 }},
		{"negative quantity", func(f *domain.Fill) { f.Quantity = -1 }}, {"zero price", func(f *domain.Fill) { f.Price = 0 }},
		{"negative price", func(f *domain.Fill) { f.Price = -1 }}, {"timestamp", func(f *domain.Fill) { f.Timestamp = time.Time{} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := valid
			tt.edit(&f)
			if err := f.Validate(); err == nil {
				t.Fatal("invalid fill accepted")
			}
		})
	}
}
