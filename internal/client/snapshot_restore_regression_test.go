package client

import (
	"errors"
	"testing"
)

func TestReplaceSnapshotTxPreservesTrafficForRetainedBindings(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	current, err := repo.Create(Client{Name: "kept", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: current.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSample(Sample{
		BindingID: binding.ID, UploadBytes: 11, DownloadBytes: 22, AtUnix: 10,
		Monotonic: true, ProviderKey: "hysteria2:hy2:" + binding.ID,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot := current
	snapshot.Name = "rolled-back"
	snapshot.Version = 1
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{snapshot}, []Binding{binding}, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	up, down, err := store.TotalsForClient(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 11 || down != 22 {
		t.Fatalf("retained binding traffic after rollback totals=%d/%d, want 11/22", up, down)
	}
	for _, table := range []string{"traffic_counters", "traffic_samples", "traffic_runtime_state"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s rows=%d err=%v, want 1", table, count, err)
		}
	}
	got, err := repo.Get(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "rolled-back" {
		t.Fatalf("client name=%q, want rolled-back", got.Name)
	}
}

func TestReplaceSnapshotTxCleansUpRemovedBindingTraffic(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	current, err := repo.Create(Client{Name: "owner", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := repo.CreateBinding(Binding{ClientID: current.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSample(Sample{
		BindingID: removed.ID, UploadBytes: 5, AtUnix: 10,
		Monotonic: true, ProviderKey: "hysteria2:hy2:" + removed.ID,
	}); err != nil {
		t.Fatal(err)
	}

	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{current}, nil, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"traffic_counters", "traffic_samples", "traffic_runtime_state"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("removed binding left rows in %s: count=%d err=%v", table, count, err)
		}
	}
}

func TestReplaceSnapshotTxBumpsVersionsSoStaleDraftsConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	original, err := repo.Create(Client{Name: "original", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: original.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	staleClient := original
	staleClient.Name = "old-pre-rollback-draft"
	second, err := repo.Update(Client{
		ID: original.ID, Name: "second", Enabled: true, QuotaResetPolicy: ResetNever, Notes: original.Notes,
	}, original.Version)
	if err != nil {
		t.Fatal(err)
	}
	third, err := repo.Update(Client{
		ID: original.ID, Name: "third", Enabled: true, QuotaResetPolicy: ResetNever,
	}, second.Version)
	if err != nil {
		t.Fatal(err)
	}
	staleBinding := binding
	staleBinding.Enabled = false
	liveBinding, err := repo.UpdateBinding(Binding{ID: binding.ID, Enabled: true, ProtocolSettings: binding.ProtocolSettings}, binding.Version)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := original
	snapshot.Version = 1
	snapshotBinding := binding
	snapshotBinding.Version = 1
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{snapshot}, []Binding{snapshotBinding}, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	restored, err := repo.Get(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != "original" {
		t.Fatalf("restored name=%q", restored.Name)
	}
	if restored.Version <= third.Version {
		t.Fatalf("rollback reused version %d, live was %d", restored.Version, third.Version)
	}
	_, err = repo.Update(staleClient, 2)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale pre-rollback draft: err=%v, want version conflict", err)
	}
	fresh, err := repo.Update(Client{
		ID: original.ID, Name: "post-rollback", Enabled: true, QuotaResetPolicy: ResetNever,
	}, restored.Version)
	if err != nil {
		t.Fatalf("fresh post-rollback edit: %v", err)
	}

	restoredBinding, err := repo.GetBinding(binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredBinding.Version <= liveBinding.Version {
		t.Fatalf("binding rollback reused version %d, live was %d", restoredBinding.Version, liveBinding.Version)
	}
	_, err = repo.UpdateBinding(staleBinding, staleBinding.Version)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale pre-rollback binding draft: err=%v, want version conflict", err)
	}
	if _, err := repo.UpdateBinding(Binding{
		ID: binding.ID, Enabled: true, ProtocolSettings: restoredBinding.ProtocolSettings,
	}, restoredBinding.Version); err != nil {
		t.Fatalf("fresh post-rollback binding edit: %v", err)
	}
	if fresh.Name != "post-rollback" {
		t.Fatalf("fresh edit name=%q", fresh.Name)
	}
}
