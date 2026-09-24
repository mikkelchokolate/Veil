package client

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// Regression for issue #781: a durable pending/rollover target must carry the
// reset semantics that were folded into the payload hash. Replaying a stored
// target while the fresh plan reports no change (needed=false) must still run
// the period reset instead of dropping ResetPeriod/NextResetAt/period start.
func TestReconcilerPendingResetTargetPreservesResetSemantics(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)

	quota := int64(1000)
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour).Unix()
	created, err := repo.Create(Client{
		Name: "pending-rollover", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetMonthly, QuotaResetAt: &past, Depleted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "inbound", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	nextBoundary, err := nextQuotaBoundary(ResetMonthly, now)
	if err != nil {
		t.Fatal(err)
	}
	wantPeriodStart := quotaPeriodStartUnix(ResetMonthly, nextBoundary)
	// Usage below quota but inside the expired period: the rollover reset must
	// still retire it when the durable target replays.
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 500, AtUnix: wantPeriodStart - 3600}); err != nil {
		t.Fatal(err)
	}
	if up, _, err := traffic.TotalsForClient(created.ID); err != nil || up != 500 {
		t.Fatalf("precondition totals=(%d,_) err=%v, want 500 upload", up, err)
	}

	fail := true
	reconciler := NewTransactionalReconciler(repo, traffic, 0, func(mutation QuotaMutation) error {
		if fail {
			return errors.New("apply pending")
		}
		return traffic.WithRecordLock(func() error {
			return repo.WithTx(func(tx *Tx) error {
				if mutation.ResetPeriod {
					if err := ResetQuotaPeriodTx(tx, mutation.ClientID, mutation.CurrentPeriodStart); err != nil {
						return err
					}
				}
				return ApplyQuotaMutationTx(tx, mutation)
			})
		})
	})
	reconciler.now = func() time.Time { return now }

	if _, err := reconciler.ReconcileOnce(); err == nil {
		t.Fatal("first reconcile should surface the pending apply failure")
	}

	// The durable row must persist the reset semantics alongside the hash.
	var resetPeriod int
	var nextReset sql.NullInt64
	var periodStart int64
	if err := db.QueryRow(`SELECT target_reset_period,target_next_reset_at,target_period_start
FROM quota_enforcement WHERE client_id=? AND state<>'superseded'`, created.ID).
		Scan(&resetPeriod, &nextReset, &periodStart); err != nil {
		t.Fatal(err)
	}
	if resetPeriod != 1 || !nextReset.Valid || nextReset.Int64 != nextBoundary || periodStart != wantPeriodStart {
		t.Fatalf("durable reset target lost fields: reset=%d next=%+v start=%d (want start=%d)",
			resetPeriod, nextReset, periodStart, wantPeriodStart)
	}

	// Force needed=false for the next reconcile: the target's client-visible
	// fields already match while the period counters were never retired (the
	// crash window this replay path exists for).
	if _, err := db.Exec(`UPDATE clients SET depleted=0, quota_reset_at=? WHERE id=?`, nextBoundary, created.ID); err != nil {
		t.Fatal(err)
	}

	fail = false
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("pending replay reconcile: %v", err)
	}

	up, down, err := traffic.TotalsForClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 0 || down != 0 {
		t.Fatalf("replayed reset target did not retire period counters: up=%d down=%d", up, down)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=? AND state<>'superseded'`, created.ID).
		Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "enforced" {
		t.Fatalf("replayed target state=%q, want enforced", state)
	}
	reloaded, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.QuotaResetAt == nil || *reloaded.QuotaResetAt != nextBoundary {
		t.Fatalf("quotaResetAt=%v, want next boundary %d", reloaded.QuotaResetAt, nextBoundary)
	}
}
