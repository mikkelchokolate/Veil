package privileged

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestRotateStateKeyReencryptsStateAndBacksUp(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")

	oldKey := make([]byte, secrets.KeySize)
	if _, err := rand.Read(oldKey); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	var oldKeyArray [secrets.KeySize]byte
	copy(oldKeyArray[:], oldKey)
	cipher, err := secrets.NewCipher(oldKeyArray)
	if err != nil {
		t.Fatal(err)
	}
	stateBody, err := managementstate.NewStore(statePath, cipher).Marshal(model.ManagementSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBody, 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "veil.db")
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := rotateStateKey(statePath, keyPath, func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) }); err != nil {
		t.Fatalf("rotate state key: %v", err)
	}

	newKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(newKey) == string(oldKey) {
		t.Fatal("expected new key to differ from old key")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	if _, err := os.Stat(statePath + ".pre-rotation-20260605T120000.000000000Z"); err != nil {
		t.Fatalf("state safety backup missing: %v", err)
	}
	if _, err := os.Stat(keyPath + ".pre-rotation-20260605T120000.000000000Z"); err != nil {
		t.Fatalf("key safety backup missing: %v", err)
	}
	rotatedState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
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
	digest, err := apply.NewSnapshotStore(db).StateDigest(revisions.Desired)
	if err != nil {
		t.Fatal(err)
	}
	if digest != managementstate.EncodedStateSHA256(rotatedState) {
		t.Fatalf("rotated snapshot digest=%q", digest)
	}
}

func TestRotateStateKeyRevisionFailureRestoresStateAndKey(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	oldKey := make([]byte, secrets.KeySize)
	if _, err := rand.Read(oldKey); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	var oldKeyArray [secrets.KeySize]byte
	copy(oldKeyArray[:], oldKey)
	cipher, err := secrets.NewCipher(oldKeyArray)
	if err != nil {
		t.Fatal(err)
	}
	oldState, err := managementstate.NewStore(statePath, cipher).Marshal(model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "before-rotation.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, oldState, 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "veil.db")
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TRIGGER sabotage_rotation_revision
BEFORE UPDATE OF desired_revision ON revisions
BEGIN
  SELECT RAISE(ABORT, 'sabotaged revisions table');
END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := rotateStateKey(statePath, keyPath, func() time.Time {
		return time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)
	}); err == nil {
		t.Fatal("key rotation succeeded after revision sabotage")
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfter, oldState) {
		t.Fatal("failed key rotation did not restore exact old state")
	}
	if !bytes.Equal(keyAfter, oldKey) {
		t.Fatal("failed key rotation did not restore old key")
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
	if revisions.Desired != 0 || apply.NewSnapshotStore(db).Has(1) {
		t.Fatalf("failed key rotation changed SQLite boundary: %+v", revisions)
	}
}

func TestRotateStateKeyRejectsWrongKeyLength(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateStateKey(statePath, keyPath, time.Now); err == nil {
		t.Fatal("expected wrong key length to fail")
	}
}

func TestRotateStateKeyRejectsMissingState(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	key := make([]byte, secrets.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateStateKey(statePath, keyPath, time.Now); err == nil {
		t.Fatal("expected missing state error")
	}
}

func TestRotateStateKeyRejectsInvalidState(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	key := make([]byte, secrets.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("not-a-valid-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateStateKey(statePath, keyPath, time.Now); err == nil {
		t.Fatal("expected invalid state error")
	}
}

func TestRotateStateKeyRejectsMissingKey(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	if err := rotateStateKey(statePath, keyPath, time.Now); err == nil {
		t.Fatal("expected missing key error")
	}
}
