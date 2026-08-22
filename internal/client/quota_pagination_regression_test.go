package client

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestQuotaReconcilerPaginatesAllTenThousandAndOneClients(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "veil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	clients, err := tx.Prepare(`INSERT INTO clients
	  (id,name,enabled,quota_bytes,quota_reset_policy,depleted,created_at,updated_at,version)
	  VALUES(?,?,1,1,'never',0,?,?,1)`)
	if err != nil {
		t.Fatal(err)
	}
	counters, err := tx.Prepare(`INSERT INTO traffic_counters
	  (client_id,binding_id,upload_bytes,download_bytes,last_observed_at,telemetry_state,updated_at)
	  VALUES(?,'',2,0,?,'observed',?)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for index := 0; index < 10001; index++ {
		id := fmt.Sprintf("quota-scale-%05d", index)
		if _, err := clients.Exec(id, id, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := counters.Exec(id, now, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = clients.Close()
	_ = counters.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var mutations atomic.Int64
	reconciler := NewTransactionalReconciler(NewRepository(db), NewTrafficStore(db), 0, func(mutation QuotaMutation) error {
		if !mutation.Depleted {
			t.Errorf("client %s was not marked depleted", mutation.ClientID)
		}
		mutations.Add(1)
		return nil
	})
	changed, err := reconciler.ReconcileOnce()
	if err != nil {
		t.Fatal(err)
	}
	if changed != 10001 || mutations.Load() != 10001 {
		t.Fatalf("reconciled=%d callbacks=%d want=10001", changed, mutations.Load())
	}
}
