package statecommit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestSaveCommitsStateRevisionSnapshotAndDigest(t *testing.T) {
	statePath, databasePath, cipher := stateCommitFixture(t)
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "cli.example.com"},
		Users:    []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}
	revision, err := Save(snapshot, Options{StatePath: statePath, DatabasePath: databasePath, Cipher: cipher})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		t.Fatalf("revision=%d want=1", revision)
	}
	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Desired != revision || revisions.Applied != 0 {
		t.Fatalf("revisions=%+v", revisions)
	}
	snapshotStore := apply.NewSnapshotStore(db)
	digest, err := snapshotStore.StateDigest(revision)
	if err != nil {
		t.Fatal(err)
	}
	if digest != managementstate.EncodedStateSHA256(stateBody) {
		t.Fatalf("snapshot digest=%q", digest)
	}
	payload, err := snapshotStore.Load(revision)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"username":"admin"`)) {
		t.Fatalf("immutable snapshot does not contain committed user: %s", payload)
	}
}

func TestSaveRevisionFailureRestoresPreviousState(t *testing.T) {
	statePath, databasePath, cipher := stateCommitFixture(t)
	first := model.ManagementSnapshot{Settings: model.Settings{Mode: "dev", Domain: "before.example.com"}}
	if _, err := Save(first, Options{StatePath: statePath, DatabasePath: databasePath, Cipher: cipher}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TRIGGER sabotage_statecommit_revision
BEFORE UPDATE OF desired_revision ON revisions
BEGIN
  SELECT RAISE(ABORT, 'sabotaged revisions table');
END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	second := model.ManagementSnapshot{Settings: model.Settings{Mode: "dev", Domain: "rejected.example.com"}}
	if _, err := Save(second, Options{StatePath: statePath, DatabasePath: databasePath, Cipher: cipher}); err == nil {
		t.Fatal("sabotaged out-of-process state mutation succeeded")
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("state bytes changed after failed out-of-process mutation")
	}
	db, err = storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Desired != 1 {
		t.Fatalf("desired revision=%d want=1", revisions.Desired)
	}
	if apply.NewSnapshotStore(db).Has(2) {
		t.Fatal("failed out-of-process mutation left revision 2 snapshot")
	}
	for _, path := range []string{statePath + ".pending-mutation.json", statePath + ".pending-mutation.previous"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed mutation left recovery artifact %s: %v", path, err)
		}
	}
}

func stateCommitFixture(t *testing.T) (string, string, *secrets.Cipher) {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	databasePath := filepath.Join(root, "veil.db")
	var key [secrets.KeySize]byte
	for index := range key {
		key[index] = byte(index + 1)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{Settings: model.Settings{Mode: "dev"}}); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return statePath, databasePath, cipher
}
