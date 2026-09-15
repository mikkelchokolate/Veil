package client

import (
	"errors"
	"testing"
	"time"
)

var errTestApplyFailed = errors.New("apply pending")

func TestQuotaMutationRejectsStalePlanAfterAdminVersionBump(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(1000)
	created, err := repo.Create(Client{
		Name: "stale-plan", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 1200, AtUnix: 1}); err != nil {
		t.Fatal(err)
	}

	reconciler := NewTransactionalReconciler(repo, traffic, 0, func(mutation QuotaMutation) error {
		current, err := repo.Get(mutation.ClientID)
		if err != nil {
			return err
		}
		raised := int64(10000)
		current.QuotaBytes = &raised
		if _, err := repo.Update(current, current.Version); err != nil {
			return err
		}
		return repo.WithTx(func(tx *Tx) error {
			return ApplyQuotaMutationTx(tx, mutation)
		})
	})
	if _, err := reconciler.ReconcileOnce(); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale depletion plan: err=%v, want version conflict", err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Depleted {
		t.Fatal("stale depletion plan disabled a client after its quota was increased")
	}
	if got.QuotaBytes == nil || *got.QuotaBytes != 10000 {
		t.Fatalf("quota=%v, want 10000", got.QuotaBytes)
	}
}

func TestQuotaMutationRejectsStaleRolloverAfterResetAtChange(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(1000)
	past := time.Now().UTC().Add(-time.Hour).Unix()
	created, err := repo.Create(Client{
		Name: "stale-reset", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &past,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 40, AtUnix: past - 60}); err != nil {
		t.Fatal(err)
	}

	reconciler := NewTransactionalReconciler(repo, traffic, 0, func(mutation QuotaMutation) error {
		current, err := repo.Get(mutation.ClientID)
		if err != nil {
			return err
		}
		postponed := time.Now().UTC().Add(24 * time.Hour).Unix()
		current.QuotaResetAt = &postponed
		if _, err := repo.Update(current, current.Version); err != nil {
			return err
		}
		return repo.WithTx(func(tx *Tx) error {
			if mutation.ResetPeriod {
				if err := ResetQuotaPeriodTx(tx, mutation.ClientID, mutation.CurrentPeriodStart); err != nil {
					return err
				}
			}
			return ApplyQuotaMutationTx(tx, mutation)
		})
	})
	if _, err := reconciler.ReconcileOnce(); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale rollover plan: err=%v, want version conflict", err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.QuotaResetAt == nil || *got.QuotaResetAt <= time.Now().UTC().Unix() {
		t.Fatalf("quotaResetAt=%v, want postponed future timestamp", got.QuotaResetAt)
	}
	up, _, err := traffic.TotalsForClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 40 {
		t.Fatalf("stale rollover cleared counters to %d", up)
	}
}
