package client

import (
	"errors"
	"testing"
	"time"
)

func TestQuotaPeriodResetSupersedesPendingDepletedTarget(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)

	quota := int64(100)
	future := time.Now().UTC().Add(time.Hour).Unix()
	created, err := repo.Create(Client{
		Name: "supersede", Enabled: true, QuotaBytes: &quota,
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
			if err := repo.SetDepleted(mutation.ClientID, mutation.Depleted); err != nil {
				return err
			}
			return errors.New("apply pending")
		}
		return repo.SetDepleted(mutation.ClientID, mutation.Depleted)
	})
	if _, err := reconciler.ReconcileOnce(); err == nil {
		t.Fatal("first depleted target should remain pending after apply failure")
	}

	current, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour).Unix()
	current.QuotaResetAt = &past
	if _, err := repo.Update(current, current.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("reset reconcile: %v", err)
	}

	rows, err := db.Query(`SELECT target_depleted,state,target_generation,target_payload_hash FROM quota_enforcement WHERE client_id=? ORDER BY target_generation`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type target struct {
		depleted   int
		state      string
		generation int64
		hash       string
	}
	var targets []target
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.depleted, &item.state, &item.generation, &item.hash); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, item)
	}
	if len(targets) != 2 {
		t.Fatalf("targets=%+v, want old and replacement", targets)
	}
	if targets[0].depleted != 1 || targets[0].state != "superseded" {
		t.Fatalf("old target=%+v, want depleted superseded", targets[0])
	}
	if targets[1].depleted != 0 || targets[1].state != "enforced" {
		t.Fatalf("replacement=%+v, want non-depleted enforced", targets[1])
	}
	if targets[0].generation >= targets[1].generation || targets[0].hash == targets[1].hash {
		t.Fatalf("targets are not distinct monotonic generations: %+v", targets)
	}
}
