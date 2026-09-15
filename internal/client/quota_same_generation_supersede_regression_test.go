package client

import (
	"testing"
	"time"
)

func TestSameGenerationQuotaReplacementClearsSupersededHash(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(100)
	future := time.Now().UTC().Add(time.Hour).Unix()
	created, err := repo.Create(Client{
		Name: "same-gen", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &future,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 200, AtUnix: 1}); err != nil {
		t.Fatal(err)
	}

	var attempts int
	reconciler := NewTransactionalReconciler(repo, traffic, 0, func(mutation QuotaMutation) error {
		attempts++
		if attempts == 1 {
			if mutation.ResetPeriod || !mutation.Depleted {
				t.Fatalf("first plan=%+v, want depleted without reset", mutation)
			}
			return errTestApplyFailed
		}
		if !mutation.ResetPeriod || mutation.Depleted {
			t.Fatalf("replacement plan=%+v, want reset without depleted", mutation)
		}
		return repo.WithTx(func(tx *Tx) error {
			if err := ResetQuotaPeriodTx(tx, mutation.ClientID, mutation.CurrentPeriodStart); err != nil {
				return err
			}
			return ApplyQuotaMutationTx(tx, mutation)
		})
	})
	if _, err := reconciler.ReconcileOnce(); err == nil {
		t.Fatal("first depleted apply should fail without a version bump")
	}
	if created.Version != 1 {
		t.Fatalf("precondition version=%d", created.Version)
	}
	past := time.Now().UTC().Add(-time.Hour).Unix()
	if _, err := db.Exec(`UPDATE clients SET quota_reset_at=? WHERE id=?`, past, created.ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != created.Version {
		t.Fatalf("version bumped to %d, want %d so generation stays the same", got.Version, created.Version)
	}
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("replacement reconcile: %v", err)
	}

	rows, err := db.Query(`SELECT state,target_generation,target_payload_hash,target_depleted FROM quota_enforcement WHERE client_id=? ORDER BY id`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type target struct {
		state      string
		generation int64
		hash       string
		depleted   int
	}
	var targets []target
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.state, &item.generation, &item.hash, &item.depleted); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, item)
	}
	if len(targets) != 1 {
		t.Fatalf("targets=%+v, want one replaced row at the original generation", targets)
	}
	if targets[0].generation != int64(created.Version)+1 {
		t.Fatalf("generation=%d, want %d", targets[0].generation, created.Version+1)
	}
	if targets[0].state != "enforced" || targets[0].depleted != 0 {
		t.Fatalf("replacement=%+v, want enforced non-depleted", targets[0])
	}
	restored, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Depleted {
		t.Fatal("rollover left the client depleted")
	}
	up, down, err := traffic.TotalsForClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 0 || down != 0 {
		t.Fatalf("reset totals=%d/%d, want 0/0 for samples before the elapsed boundary", up, down)
	}
}
