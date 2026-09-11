package client

import (
	"errors"
	"testing"
)

func TestUpdateClearsExhaustedAndScheduledQuota(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	quota := int64(1000)
	resetAt := int64(1_700_000_000)
	notes := "keep-me"
	created, err := repo.Create(Client{
		Name: "quota-clear", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &resetAt, Depleted: true, Notes: notes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,target_period_epoch,state,updated_at)
  VALUES(?,1,printf('%064x',1),1,?, 'pending', 0)`, created.ID, resetAt); err != nil {
		t.Fatal(err)
	}

	created.QuotaBytes = nil
	view, err := svc.Update(created, created.Version)
	if err != nil {
		t.Fatalf("clear quota: %v", err)
	}
	if view.QuotaBytes != nil {
		t.Fatalf("quotaBytes=%v, want nil", view.QuotaBytes)
	}
	if view.Depleted {
		t.Fatal("depleted still set")
	}
	if view.QuotaResetAt != nil {
		t.Fatalf("quotaResetAt=%v, want nil", view.QuotaResetAt)
	}
	if view.QuotaResetPolicy != ResetNever {
		t.Fatalf("quotaResetPolicy=%q", view.QuotaResetPolicy)
	}
	if view.Notes != notes {
		t.Fatalf("notes=%q, want preserved", view.Notes)
	}

	var state string
	if err := db.QueryRow(`SELECT state FROM quota_enforcement WHERE client_id=?`, created.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "superseded" {
		t.Fatalf("enforcement state=%q, want superseded", state)
	}

	_, err = svc.Update(view.Client, created.Version)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version: got %v, want ErrVersionConflict", err)
	}
}
