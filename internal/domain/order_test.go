package domain_test

import (
	"testing"
	"time"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func TestOrderValidation(t *testing.T) {
	valid := domain.Order{OrderID: "order-1", IntentID: "intent-1", Symbol: "AAPL", Side: domain.Buy, Quantity: 1, Status: domain.OrderNew, CreatedAt: time.Date(2026, 9, 15, 13, 30, 0, 0, time.UTC)}
	for _, status := range []domain.OrderStatus{domain.OrderNew, domain.OrderSubmitted, domain.OrderCancelled, domain.OrderRejected, domain.OrderFilled} {
		o := valid
		o.Status = status
		if err := o.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name string
		edit func(*domain.Order)
	}{
		{"empty order ID", func(o *domain.Order) { o.OrderID = "" }},
		{"blank order ID", func(o *domain.Order) { o.OrderID = " \t" }},
		{"intent ID", func(o *domain.Order) { o.IntentID = "" }},
		{"symbol", func(o *domain.Order) { o.Symbol = "" }},
		{"side", func(o *domain.Order) { o.Side = "OTHER" }},
		{"quantity", func(o *domain.Order) { o.Quantity = 0 }},
		{"negative quantity", func(o *domain.Order) { o.Quantity = -1 }},
		{"timestamp", func(o *domain.Order) { o.CreatedAt = time.Time{} }},
		{"empty status", func(o *domain.Order) { o.Status = "" }},
		{"unknown status", func(o *domain.Order) { o.Status = "UNKNOWN" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := valid
			tt.edit(&o)
			if err := o.Validate(); err == nil {
				t.Fatal("invalid order accepted")
			}
		})
	}
}
