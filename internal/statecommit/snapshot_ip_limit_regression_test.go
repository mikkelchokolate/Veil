package statecommit

import (
	"encoding/json"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// A CLI state commit (admin set, install apply, repair, key rotation) freezes
// the normalized clients into the revision's immutable snapshot. That snapshot
// used to drop ipLimit, so rolling back to a CLI-committed revision restored
// every client as IP-unlimited while deviceLimit survived.
func TestSaveSnapshotKeepsClientConnectionLimits(t *testing.T) {
	statePath, databasePath, cipher := stateCommitFixture(t)
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	deviceLimit, ipLimit := 2, 3
	created, err := client.NewRepository(db).Create(client.Client{
		Name: "limited", Enabled: true, QuotaResetPolicy: client.ResetNever,
		DeviceLimit: &deviceLimit, IPLimit: &ipLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	revision, err := Save(model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "cli.example.com"},
		Users:    []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}, Options{StatePath: statePath, DatabasePath: databasePath, Cipher: cipher})
	if err != nil {
		t.Fatal(err)
	}

	db, err = storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload, err := apply.NewSnapshotStore(db).Load(revision)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot model.ManagementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Clients) != 1 || snapshot.Clients[0].ID != created.ID {
		t.Fatalf("snapshot clients = %+v, want the one created client", snapshot.Clients)
	}
	got := snapshot.Clients[0]
	if got.DeviceLimit == nil || *got.DeviceLimit != deviceLimit {
		t.Fatalf("snapshot deviceLimit = %v, want %d", got.DeviceLimit, deviceLimit)
	}
	if got.IPLimit == nil || *got.IPLimit != ipLimit {
		t.Fatalf("snapshot ipLimit = %v, want %d (dropped by the state-commit converter)", got.IPLimit, ipLimit)
	}
}
