package domain_test

import (
	"math"
	"testing"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func TestNotional(t *testing.T) {
	for _, tt := range []struct {
		name     string
		price    domain.Price
		quantity domain.Quantity
		want     domain.Money
		wantErr  bool
	}{
		{"whole shares", 1002500, 3, 3007500, false},
		{"empty position", 1002500, 0, 0, false},
		{"maximum exact", math.MaxInt64, 1, math.MaxInt64, false},
		{"maximum quantity", 1, math.MaxInt64, math.MaxInt64, false},
		{"below multiplication boundary", math.MaxInt64 / 2, 2, math.MaxInt64 - 1, false},
		{"overflow", math.MaxInt64/2 + 1, 2, 0, true},
		{"zero price", 0, 1, 0, true},
		{"negative price", -1, 1, 0, true},
		{"negative quantity", 1, -1, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.Notional(tt.price, tt.quantity)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Notional = %d, %v; want %d, error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestMoneyString(t *testing.T) {
	for _, tt := range []struct {
		value domain.Money
		want  string
	}{
		{0, "0.0000"}, {1, "0.0001"}, {1234567, "123.4567"}, {-1234567, "-123.4567"},
		{math.MaxInt64, "922337203685477.5807"}, {math.MinInt64, "-922337203685477.5808"},
	} {
		if got := tt.value.String(); got != tt.want {
			t.Errorf("String = %q, want %q", got, tt.want)
		}
	}
}
