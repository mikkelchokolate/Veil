package privileged

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
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
