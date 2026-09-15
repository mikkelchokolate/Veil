package client

import (
	"testing"
	"time"
)

func TestComputeStatusApplyFlagsHavePriority(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	active := Client{Name: "a", Enabled: true}
	if got := ComputeStatus(active, now, true, true, true); got != StatusApplyFailed {
		t.Fatalf("apply_failed priority: got %s", got)
	}
	if got := ComputeStatus(active, now, false, true, false); got != StatusPendingApply {
		t.Fatalf("pending_apply: got %s", got)
	}
	if got := ComputeStatus(active, now, false, false, false); got != StatusActive {
		t.Fatalf("active: got %s", got)
	}
}

func TestServiceViewUsesApplyReadiness(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	creds := NewCredentialStore(db, cipher)
	created, err := repo.Create(Client{Name: "apply-view", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "in-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, creds).WithApplyReadiness(func(id string) (bool, bool) {
		if id != created.ID {
			t.Fatalf("unexpected id %s", id)
		}
		return true, false
	})
	view, err := svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusApplyFailed {
		t.Fatalf("status=%s, want apply_failed", view.Status)
	}
}
