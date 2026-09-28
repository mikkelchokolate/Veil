package client

import (
	"errors"
	"testing"
	"time"
)

// TestExpiresAtNonPositiveRejected (#1108): a supplied expiresAt of 0 or a
// negative value has no valid semantics — ComputeStatus treated it as
// "active forever" while enforcement paths treated it as already expired.
// The service must reject it instead of persisting an ambiguous row.
func TestExpiresAtNonPositiveRejected(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	svc := NewService(repo, NewCredentialStore(db, cipher))

	for _, value := range []int64{0, -1, -1_700_000_000} {
		expires := value
		_, err := svc.Create(Client{Name: "bad-expiry", Enabled: true, QuotaResetPolicy: ResetNever, ExpiresAt: &expires})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("create expiresAt=%d: got %v, want ErrValidation", value, err)
		}
	}

	valid := int64(1_800_000_000)
	created, err := svc.Create(Client{Name: "ok-expiry", Enabled: true, QuotaResetPolicy: ResetNever, ExpiresAt: &valid})
	if err != nil {
		t.Fatalf("create valid: %v", err)
	}
	zero := int64(0)
	c := created.Client
	c.ExpiresAt = &zero
	if _, err := svc.Update(c, created.Version); !errors.Is(err, ErrValidation) {
		t.Fatalf("update expiresAt=0: got %v, want ErrValidation", err)
	}
}

// TestComputeStatusNonPositiveExpiryAlignsWithEnforcement (#1108): a row that
// predates the strict validation (expires_at=0 from an older version) is
// read as expired by every enforcement path — ComputeStatus must agree so a
// stored row can never report "active" while being filtered from runtime.
func TestComputeStatusNonPositiveExpiryAlignsWithEnforcement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, value := range []int64{0, -5, now.Unix()} {
		expires := value
		c := Client{Enabled: true, ExpiresAt: &expires}
		if got := ComputeStatus(c, now, false, false, false); got != StatusExpired {
			t.Fatalf("expiresAt=%d: status %s, want %s", value, got, StatusExpired)
		}
	}
	future := now.Unix() + 60
	c := Client{Enabled: true, ExpiresAt: &future}
	if got := ComputeStatus(c, now, false, false, false); got != StatusActive {
		t.Fatalf("future expiresAt: status %s, want %s", got, StatusActive)
	}
}
