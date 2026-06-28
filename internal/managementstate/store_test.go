package managementstate

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestStoreRoundTripsSetupMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, nil)
	want := model.SetupState{Completed: true, CompletedAt: "2026-06-05T12:00:00Z"}
	if err := store.Save(model.ManagementSnapshot{Setup: want}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := store.Load()
	if err != nil || !ok || got.Setup != want {
		t.Fatalf("setup = %+v, ok=%v, err=%v", got.Setup, ok, err)
	}
}

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

func TestStoreSavePreservesOwnershipAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, nil)
	if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Set non-default ownership and permissions on the existing file.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	// Chown requires root; skip the UID/GID assertion when running as non-root.
	var wantUID, wantGID int = -1, -1
	if os.Getuid() == 0 {
		wantUID, wantGID = 0, 0
		if err := os.Chown(path, wantUID, wantGID); err != nil {
			t.Fatalf("Chown: %v", err)
		}
	}

	if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:31337", Mode: "dev"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	if os.Getuid() == 0 {
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("expected *syscall.Stat_t")
		}
		if int(st.Uid) != wantUID || int(st.Gid) != wantGID {
			t.Fatalf("owner = %d:%d, want %d:%d", st.Uid, st.Gid, wantUID, wantGID)
		}
	}

	loaded, ok, err := store.Load()
	if err != nil || !ok || loaded.Settings.PanelListen != "127.0.0.1:31337" {
		t.Fatalf("Load = %+v ok=%v err=%v", loaded, ok, err)
	}
}
