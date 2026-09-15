package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Price is an int64 count of 0.0001 US dollars (10,000 units per dollar).
// Four decimal places preserve cents and sub-cent quote precision exactly;
// this representation does not enforce exchange tick-size rules.
// ParsePrice accepts zero, but quotes require strictly positive prices.
type Price int64

// ParsePrice accepts ASCII digits with an optional decimal point followed by
// one to four digits. Signs, whitespace, exponents, and rounding are unsupported.
// Values outside the nonnegative int64 range are rejected.
func ParsePrice(s string) (Price, error) {
	whole, fraction, hasPoint := strings.Cut(s, ".")
	if whole == "" || (hasPoint && (fraction == "" || len(fraction) > 4)) {
		return 0, fmt.Errorf("invalid price %q", s)
	}
	for _, part := range []string{whole, fraction} {
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return 0, fmt.Errorf("invalid price %q", s)
			}
		}
	}
	units, err := strconv.ParseInt(whole+fraction+strings.Repeat("0", 4-len(fraction)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("price %q out of range: %w", s, err)
	}
	return Price(units), nil
}

// String returns a decimal with exactly four fractional digits, without rounding.
// It also formats directly constructed negative values for diagnostics.
func (p Price) String() string {
	digits := strconv.FormatInt(int64(p), 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) < 5 {
		digits = strings.Repeat("0", 5-len(digits)) + digits
	}
	return sign + digits[:len(digits)-4] + "." + digits[len(digits)-4:]
}
