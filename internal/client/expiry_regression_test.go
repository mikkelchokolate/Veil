package client

import (
	"errors"
	"math"
	"testing"
)

func TestExtendExpiresAtRejectsOverflowAndCap(t *testing.T) {
	if got, err := ExtendExpiresAt(100, MaxExpiryExtensionDays); err != nil {
		t.Fatalf("max accepted days: %v", err)
	} else if got != 100+int64(MaxExpiryExtensionDays)*secondsPerDay {
		t.Fatalf("max accepted expiry=%d", got)
	}

	if _, err := ExtendExpiresAt(100, MaxExpiryExtensionDays+1); !errors.Is(err, ErrValidation) {
		t.Fatalf("max+1 days: got %v, want ErrValidation", err)
	}

	overflowDays := math.MaxInt64/secondsPerDay + 1
	if overflowDays <= int64(MaxExpiryExtensionDays) {
		t.Fatalf("test invariant: overflow days %d should exceed product cap", overflowDays)
	}
	if overflowDays <= math.MaxInt {
		if _, err := ExtendExpiresAt(0, int(overflowDays)); !errors.Is(err, ErrValidation) {
			t.Fatalf("multiplication overflow days: got %v, want ErrValidation", err)
		}
	}

	if _, err := ExtendExpiresAt(math.MaxInt64-10, 1); !errors.Is(err, ErrValidation) {
		t.Fatalf("addition overflow: got %v, want ErrValidation", err)
	}

	if _, err := ExtendExpiresAt(50, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero days: got %v, want ErrValidation", err)
	}
	if _, err := ExtendExpiresAt(50, -3); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative days: got %v, want ErrValidation", err)
	}

	got, err := ExtendExpiresAt(1_000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1_000+2*secondsPerDay {
		t.Fatalf("got %d", got)
	}
}
