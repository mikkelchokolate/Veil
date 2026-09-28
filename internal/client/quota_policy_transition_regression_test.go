package client

import (
	"strings"
	"testing"
)

// TestQuotaResetPolicyChangeClearsStaleBoundary (#1109): quotaResetAt is the
// boundary the reconciler computed under the OLD policy. Switching policy
// without an explicit new boundary must drop it — otherwise a monthly->daily
// change keeps a monthly boundary for weeks and the client stays depleted.
func TestQuotaResetPolicyChangeClearsStaleBoundary(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	svc := NewService(repo, NewCredentialStore(db, cipher))

	quota := int64(1 << 30)
	resetAt := int64(1_800_000_000) // stale monthly boundary, far in the future
	created, err := svc.Create(Client{
		Name: "policy-switch", Enabled: true,
		QuotaBytes: &quota, QuotaResetPolicy: ResetMonthly, QuotaResetAt: &resetAt,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// monthly -> daily without a new boundary: the stale boundary clears so
	// the reconciler reschedules under the new policy.
	c := created.Client
	c.QuotaResetPolicy = ResetDaily
	view, err := svc.Update(c, created.Version)
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if view.Client.QuotaResetAt != nil {
		t.Fatalf("quotaResetAt=%v still set after policy change, want nil", *view.Client.QuotaResetAt)
	}

	// daily -> weekly WITH an explicit new boundary: caller intent wins.
	c = view.Client
	explicit := int64(1_800_500_000)
	c.QuotaResetPolicy = ResetWeekly
	c.QuotaResetAt = &explicit
	view, err = svc.Update(c, view.Version)
	if err != nil {
		t.Fatalf("update explicit boundary: %v", err)
	}
	if view.Client.QuotaResetAt == nil || *view.Client.QuotaResetAt != explicit {
		t.Fatalf("explicit quotaResetAt not preserved: %v", view.Client.QuotaResetAt)
	}
}

// TestQuotaPolicyChangeSupersedesPendingEnforcement (#1109): a pending quota
// enforcement row bound to the old plan must be superseded when the policy
// changes, else the runtime keeps enforcing a boundary that no longer
// reflects the client's policy.
func TestQuotaPolicyChangeSupersedesPendingEnforcement(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	svc := NewService(repo, NewCredentialStore(db, cipher))

	quota := int64(1 << 30)
	resetAt := int64(1_800_000_000)
	created, err := svc.Create(Client{
		Name: "supersede-plan", Enabled: true,
		QuotaBytes: &quota, QuotaResetPolicy: ResetMonthly, QuotaResetAt: &resetAt,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO quota_enforcement (client_id, target_generation, target_payload_hash, target_depleted, state, updated_at)
	  VALUES (?, ?, ?, 0, 'pending', 0)`, created.ID, created.Version+1, strings.Repeat("0", 64)); err != nil {
		t.Fatalf("seed enforcement: %v", err)
	}

	c := created.Client
	c.QuotaResetPolicy = ResetDaily
	if _, err := svc.Update(c, created.Version); err != nil {
		t.Fatalf("update: %v", err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=?`, created.ID).Scan(&state); err != nil {
		t.Fatalf("read enforcement: %v", err)
	}
	if state != "superseded" {
		t.Fatalf("pending enforcement state=%q, want superseded", state)
	}
}
