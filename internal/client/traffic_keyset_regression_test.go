package client

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestQuotaReconcilerKeysetDoesNotSkipAfterConcurrentDelete(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)

	ids := make([]string, 0, 130)
	for index := 0; index < 130; index++ {
		quota := int64(1)
		current, err := repo.Create(Client{Name: fmt.Sprintf("keyset-%03d", index), Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever})
		if err != nil {
			t.Fatal(err)
		}
		binding, err := repo.CreateBinding(Binding{ClientID: current.ID, InboundID: "hy2", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 2, AtUnix: time.Now().Unix(), Monotonic: false}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, current.ID)
	}

	attempted := make(map[string]bool)
	var mu sync.Mutex
	deleted := false
	reconciler := NewTransactionalReconciler(repo, store, time.Hour, func(mutation QuotaMutation) error {
		mu.Lock()
		attempted[mutation.ClientID] = true
		if !deleted {
			deleted = true
			mu.Unlock()
			if err := repo.Delete(ids[0]); err != nil {
				return err
			}
			return nil
		}
		mu.Unlock()
		return nil
	})
	changed, err := reconciler.ReconcileOnce()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if changed == 0 {
		t.Fatal("expected quota changes")
	}
	for _, id := range ids[1:] {
		if !attempted[id] {
			t.Fatalf("client %s skipped after concurrent delete", id)
		}
	}
}
