package domain

import (
	"fmt"
	"math"
)

// Money is a monetary value in $0.0001 units, distinct from a per-share Price.
// PnL may be negative; account owners must enforce nonnegative cash balances.
type Money int64

// String formats signed monetary values with four decimal places.
func (m Money) String() string { return Price(m).String() }

// Notional multiplies a positive price by a nonnegative whole-share quantity.
// Zero quantity is allowed for valuing an empty position. Whole shares preserve
// the $0.0001 scale exactly; no rounding or rescaling is needed.
func Notional(price Price, quantity Quantity) (Money, error) {
	if price <= 0 {
		return 0, fmt.Errorf("notional price must be positive")
	}
	if quantity < 0 {
		return 0, fmt.Errorf("notional quantity must not be negative")
	}
	if quantity != 0 && int64(price) > math.MaxInt64/int64(quantity) {
		return 0, fmt.Errorf("notional overflows int64")
	}
	return Money(int64(price) * int64(quantity)), nil
}
