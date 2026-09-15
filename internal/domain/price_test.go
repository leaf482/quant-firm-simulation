package domain_test

import (
	"math"
	"testing"

	"github.com/leaf482/quant-firm-simulation/internal/domain"
)

func TestParsePrice(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  domain.Price
	}{
		{"100", 1000000}, {"100.25", 1002500}, {"100.1234", 1001234},
		{"0", 0}, {"0.0000", 0}, {"0.0001", 1}, {"1.2", 12000},
		{"1.234", 12340}, {"001.20", 12000},
		{"922337203685477.5807", domain.Price(math.MaxInt64)},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := domain.ParsePrice(tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("ParsePrice(%q) = %d, %v; want %d", tt.input, got, err, tt.want)
			}
		})
	}
}

func TestParsePriceRejectsInvalid(t *testing.T) {
	for _, input := range []string{
		"", "-1", "-0", "+1", " 1", "1 ", "1\n", ".25", "1.", ".",
		"1.00000", "100.12345", "1.2.3", "1e2", "NaN", "Inf", "1,000", "abc", "１",
		"922337203685477.5808", "922337203685478", "999999999999999999999999",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := domain.ParsePrice(input); err == nil {
				t.Fatalf("ParsePrice(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestPriceString(t *testing.T) {
	for _, tt := range []struct {
		price domain.Price
		want  string
	}{
		{0, "0.0000"}, {1, "0.0001"}, {1000000, "100.0000"},
		{1002500, "100.2500"}, {1001234, "100.1234"}, {-1, "-0.0001"},
		{domain.Price(math.MaxInt64), "922337203685477.5807"},
		{domain.Price(math.MinInt64), "-922337203685477.5808"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.price.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
			if tt.price >= 0 {
				got, err := domain.ParsePrice(tt.price.String())
				if err != nil || got != tt.price {
					t.Fatalf("round trip = %d, %v; want %d", got, err, tt.price)
				}
			}
		})
	}
}
