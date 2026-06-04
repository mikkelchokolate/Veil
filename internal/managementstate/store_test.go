package managementstate

import (
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestStoreSavesAndLoadsManagementStateModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, nil)
	snapshot := model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}, Inbounds: []model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443}}}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok || loaded.Settings.PanelListen != snapshot.Settings.PanelListen || len(loaded.Inbounds) != 1 {
		t.Fatalf("loaded = %+v ok=%v", loaded, ok)
	}
}

func TestStoreLoadDecryptionFailure(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Generate two different keys
	var keyA, keyB [secrets.KeySize]byte
	if _, err := rand.Read(keyA[:]); err != nil {
		t.Fatalf("failed to generate key A: %v", err)
	}
	if _, err := rand.Read(keyB[:]); err != nil {
		t.Fatalf("failed to generate key B: %v", err)
	}

	cipherA, err := secrets.NewCipher(keyA)
	if err != nil {
		t.Fatalf("failed to create cipher A: %v", err)
	}
	cipherB, err := secrets.NewCipher(keyB)
	if err != nil {
		t.Fatalf("failed to create cipher B: %v", err)
	}

	// Create a snapshot with some secret fields
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			NaivePassword:     "secret-naive-password",
			Hysteria2Password: "secret-hy2-password",
		},
	}

	// Save using cipherA
	storeA := NewStore(statePath, cipherA)
	if err := storeA.Save(snapshot); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Try to load using cipherB (wrong key)
	storeB := NewStore(statePath, cipherB)
	_, ok, err := storeB.Load()

	// It must fail with an error
	if err == nil {
		t.Fatal("expected error when loading state with wrong key, but got nil")
	}
	if ok {
		t.Fatal("expected ok to be false on load failure")
	}
}

func TestStoreLoadSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	var key [secrets.KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			NaivePassword: "secret-naive-password",
		},
	}

	store := NewStore(statePath, cipher)
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok to be true")
	}

	if loaded.Settings.NaivePassword != "secret-naive-password" {
		t.Errorf("expected NaivePassword to be %q, got %q", "secret-naive-password", loaded.Settings.NaivePassword)
	}
}
