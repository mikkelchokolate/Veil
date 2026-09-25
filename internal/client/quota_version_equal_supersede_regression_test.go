package client

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Regression for #1000: a pending quota target bound at exactly the client's
// current Version can never apply — ApplyQuotaMutationTx requires
// Version == TargetGeneration-1 — so the reconciler must supersede it instead
// of retrying an ErrVersionConflict loop forever.
func TestReconcilerSupersedesPendingTargetAtCurrentVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(1000)
	created, err := repo.Create(Client{
		Name: "gen-equal", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A pending row whose target generation equals the client's current
	// version: the version it was planned against is already consumed.
	if _, err := db.Exec(`INSERT INTO quota_enforcement(client_id,target_generation,target_payload_hash,target_depleted,state,next_retry_at,last_error,attempts,updated_at)
VALUES(?,?,?,1,'pending',0,'',1,?)`, created.ID, int64(created.Version), strings.Repeat("b", 64), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	var applies int
	reconciler := NewTransactionalReconciler(repo, traffic, 0, func(mutation QuotaMutation) error {
		applies++
		return errors.New("stale target must never be applied")
	})
	changed, err := reconciler.ReconcileOnce()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if applies != 0 {
		t.Fatalf("unwinnable target was applied %d times", applies)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=?`, created.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "superseded" {
		t.Fatalf("pending target at current version state=%q, want superseded", state)
	}
	if changed != 0 {
		t.Fatalf("changed=%d, want 0", changed)
	}
	// The next pass must be a clean no-op — no failed-row retry churn.
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("follow-up reconcile: %v", err)
	}
	if applies != 0 {
		t.Fatalf("superseded target was retried %d times", applies)
	}
}
