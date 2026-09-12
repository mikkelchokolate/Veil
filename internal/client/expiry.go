package client

import (
	"fmt"
	"math"
)

// MaxExpiryExtensionDays is the product cap for expiry extensions (100 years).
// Checked arithmetic still rejects additions that would overflow int64 even
// when days is within this cap (an existing expiry can already be near the
// timestamp limit).
const MaxExpiryExtensionDays = 36500

const secondsPerDay int64 = 86400

// ExtendExpiresAt returns base plus days, rejecting non-positive values, the
// product cap, and int64 overflow. Callers pass "now" when the current expiry
// is missing or already in the past.
func ExtendExpiresAt(base int64, days int) (int64, error) {
	if days <= 0 {
		return 0, fmt.Errorf("%w: days must be greater than 0", ErrValidation)
	}
	if days > MaxExpiryExtensionDays {
		return 0, fmt.Errorf("%w: days must be at most %d", ErrValidation, MaxExpiryExtensionDays)
	}
	seconds := int64(days) * secondsPerDay
	if base > math.MaxInt64-seconds {
		return 0, fmt.Errorf("%w: expiry overflow", ErrValidation)
	}
	return base + seconds, nil
}
