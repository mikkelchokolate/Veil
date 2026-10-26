package client

import (
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

// TestRecordSamplesSkipsBindingsDeletedSinceTheSnapshot locks #1445: the
// traffic provider map is built before an apply, and a client deletion cascades
// away its bindings while the runtime still reports them for one more
// collection. A reading for a vanished binding used to fail the whole provider
// batch, so every co-located client's usage since the last poll was dropped —
// including the apply's pre-restart flush (#1102). Such readings are skipped;
// the rest of the batch is recorded.
func TestRecordSamplesSkipsBindingsDeletedSinceTheSnapshot(t *testing.T) {
	db := testdb.Open(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)

	alice, err := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := repo.Create(Client{Name: "bob", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	aliceBinding, err := repo.CreateBinding(Binding{ClientID: alice.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	bobBinding, err := repo.CreateBinding(Binding{ClientID: bob.ID, InboundID: "hy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	// Baseline poll: both bindings are alive and establish the monotonic
	// baselines.
	if err := traffic.RecordSamples([]Sample{
		{BindingID: aliceBinding.ID, UploadBytes: 1000, AtUnix: now, Monotonic: true, ProviderKey: "rt1:" + aliceBinding.ID, RuntimeInstanceID: "rt1"},
		{BindingID: bobBinding.ID, UploadBytes: 1000, AtUnix: now, Monotonic: true, ProviderKey: "rt1:" + bobBinding.ID, RuntimeInstanceID: "rt1"},
	}); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}

	// The operator deletes bob; his binding is cascade-deleted while the
	// provider map still names it.
	if err := repo.Delete(bob.ID); err != nil {
		t.Fatalf("delete bob: %v", err)
	}

	// Next poll from the stale map: the runtime still reports the deleted
	// binding, and alice's counter grew by 49,995,000 bytes.
	err = traffic.RecordSamples([]Sample{
		{BindingID: bobBinding.ID, UploadBytes: 2000, AtUnix: now + 30, Monotonic: true, ProviderKey: "rt1:" + bobBinding.ID, RuntimeInstanceID: "rt1"},
		{BindingID: aliceBinding.ID, UploadBytes: 50000000, AtUnix: now + 30, Monotonic: true, ProviderKey: "rt1:" + aliceBinding.ID, RuntimeInstanceID: "rt1"},
	})
	if err != nil {
		t.Fatalf("a deleted binding failed the whole provider batch: %v", err)
	}
	upload, _, err := traffic.TotalsForClient(alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(49999000); upload != want {
		t.Fatalf("alice upload = %d, want %d (co-tenant usage was dropped)", upload, want)
	}
}

// TestRecordSampleStillFailsForAVanishedBinding keeps the explicit
// single-sample API strict: there the caller chose that binding.
func TestRecordSampleStillFailsForAVanishedBinding(t *testing.T) {
	db := testdb.Open(t)
	defer db.Close()
	traffic := NewTrafficStore(db)
	if err := traffic.RecordSample(Sample{BindingID: "missing", UploadBytes: 1, AtUnix: time.Now().Unix()}); err == nil {
		t.Fatal("RecordSample accepted a vanished binding")
	}
}
