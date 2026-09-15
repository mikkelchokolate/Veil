package client

import (
	"errors"
	"testing"
	"time"
)

func TestQuotaSameGenerationReplacementResurrectsSupersededTarget(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(100)
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour).Unix()
	created, err := repo.Create(Client{
		Name: "same-gen", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &future,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "inbound", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 200, AtUnix: 1}); err != nil {
		t.Fatal(err)
	}

	first := true
	reconciler := NewTransactionalReconciler(repo, traffic, 0, func(mutation QuotaMutation) error {
		if first {
			first = false
			return errors.New("apply pending before version bump")
		}
		return traffic.WithRecordLock(func() error {
			return repo.WithTx(func(tx *Tx) error {
				if mutation.ResetPeriod {
					if err := ResetQuotaPeriodTx(tx, mutation.ClientID, mutation.CurrentPeriodStart); err != nil {
						return err
					}
				}
				current, err := tx.Get(mutation.ClientID)
				if err != nil {
					return err
				}
				current.Depleted = mutation.Depleted
				if mutation.NextResetAt != nil {
					next := *mutation.NextResetAt
					current.QuotaResetAt = &next
				}
				_, err = tx.Update(current, current.Version)
				return err
			})
		})
	})
	reconciler.now = func() time.Time { return now }
	if _, err := reconciler.ReconcileOnce(); err == nil {
		t.Fatal("first depleted target should remain uncommitted")
	}
	current, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != created.Version {
		t.Fatalf("version bumped on failed mutation: %d -> %d", created.Version, current.Version)
	}

	later := now.Add(2 * time.Hour)
	reconciler.now = func() time.Time { return later }
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("replacement reconcile: %v", err)
	}

	var state string
	var depleted int
	var generation int64
	var hash string
	if err := db.QueryRow(`SELECT state,target_depleted,target_generation,target_payload_hash FROM quota_enforcement WHERE client_id=? AND state<>'superseded'`, created.ID).
		Scan(&state, &depleted, &generation, &hash); err != nil {
		t.Fatal(err)
	}
	if state != "enforced" || depleted != 0 {
		t.Fatalf("replacement target state=%s depleted=%d, want enforced non-depleted", state, depleted)
	}
	if generation != int64(created.Version)+1 {
		t.Fatalf("generation=%d, want same generation %d", generation, created.Version+1)
	}
	restored, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Depleted {
		t.Fatal("rollover must clear depleted")
	}
	if restored.QuotaResetAt == nil || *restored.QuotaResetAt <= later.Unix() {
		t.Fatalf("quotaResetAt not advanced: %+v", restored)
	}
}
