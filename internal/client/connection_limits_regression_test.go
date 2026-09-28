package client

import (
	"testing"
)

// #1173: deviceLimit/ipLimit are nullable positive-integer fields that must
// survive every persistence path — create, update, runtime credential
// resolution, and snapshot restore — so enforcement never silently drops a
// stored limit.

func TestClientConnectionLimitsRoundTrip(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewCredentialStore(db, newTestCipher(t))
	svc := NewService(repo, store)

	device, ip := 3, 2
	created, err := repo.Create(Client{
		Name: "limits", Enabled: true, DeviceLimit: &device, IPLimit: &ip,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceLimit == nil || *got.DeviceLimit != device {
		t.Fatalf("deviceLimit=%v, want %d", got.DeviceLimit, device)
	}
	if got.IPLimit == nil || *got.IPLimit != ip {
		t.Fatalf("ipLimit=%v, want %d", got.IPLimit, ip)
	}

	// Update must be able to clear and keep them independently.
	got.DeviceLimit = nil
	if _, err := repo.Update(got, got.Version); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceLimit != nil {
		t.Fatalf("deviceLimit=%v after clear, want nil", got.DeviceLimit)
	}
	if got.IPLimit == nil || *got.IPLimit != ip {
		t.Fatalf("ipLimit=%v after deviceLimit clear, want %d", got.IPLimit, ip)
	}

	// The runtime credential feed carries the limits to the auth callback.
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(binding.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	creds, err := svc.CredentialsForInbound("hy2")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 {
		t.Fatalf("runtime credentials = %+v", creds)
	}
	if creds[0].DeviceLimit != nil {
		t.Fatalf("credential deviceLimit=%v, want nil (cleared)", creds[0].DeviceLimit)
	}
	if creds[0].IPLimit == nil || *creds[0].IPLimit != ip {
		t.Fatalf("credential ipLimit=%v, want %d", creds[0].IPLimit, ip)
	}
	if creds[0].ClientID != created.ID {
		t.Fatalf("credential clientID=%q, want %q", creds[0].ClientID, created.ID)
	}
}

func TestClientSnapshotRestorePreservesConnectionLimits(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	device, ip := 5, 4
	created, err := repo.Create(Client{
		Name: "snap-limits", Enabled: true, DeviceLimit: &device, IPLimit: &ip,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Rollback writes the snapshot row over the live one — the limits must be
	// part of the restored payload, not silently dropped.
	snapshot := Client{
		ID: created.ID, Name: "snap-limits-older", Enabled: true,
		DeviceLimit: &device, IPLimit: &ip, Version: created.Version,
	}
	tx, err := repo.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceSnapshotTx(tx, []Client{snapshot}, nil, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReplaceSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceLimit == nil || *got.DeviceLimit != device {
		t.Fatalf("restored deviceLimit=%v, want %d", got.DeviceLimit, device)
	}
	if got.IPLimit == nil || *got.IPLimit != ip {
		t.Fatalf("restored ipLimit=%v, want %d", got.IPLimit, ip)
	}
}
