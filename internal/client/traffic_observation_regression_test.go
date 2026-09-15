package client

import "testing"

func TestTrafficSnapshotUsesSampleTimeNotReadClock(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	c, err := repo.Create(Client{Name: "obs", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	const observedAt int64 = 1_700_000_100
	if err := store.RecordSample(Sample{BindingID: b.ID, UploadBytes: 10, DownloadBytes: 20, AtUnix: observedAt}); err != nil {
		t.Fatal(err)
	}
	snap, err := store.SnapshotForClient(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.UploadBytes != 10 || snap.DownloadBytes != 20 {
		t.Fatalf("totals=%+v", snap)
	}
	if snap.LastObservedAt != observedAt {
		t.Fatalf("lastObservedAt=%d, want %d", snap.LastObservedAt, observedAt)
	}
}

func TestTrafficSnapshotConservativeOldestBindingTimestamp(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	c, _ := repo.Create(Client{Name: "multi", Enabled: true, QuotaResetPolicy: ResetNever})
	first, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-a", Enabled: true})
	second, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-b", Enabled: true})
	if err := store.RecordSample(Sample{BindingID: first.ID, UploadBytes: 1, AtUnix: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSample(Sample{BindingID: second.ID, UploadBytes: 2, AtUnix: 5000}); err != nil {
		t.Fatal(err)
	}
	snap, err := store.SnapshotForClient(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.LastObservedAt != 1000 {
		t.Fatalf("conservative collectedAt=%d, want oldest binding 1000", snap.LastObservedAt)
	}
	if snap.NewestObservedAt != 5000 {
		t.Fatalf("newest=%d, want 5000", snap.NewestObservedAt)
	}
}
