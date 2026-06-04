package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestAdminRotateKeySuccessInPlace(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")

	// Generate initial key and state
	var initKey [secrets.KeySize]byte
	for i := range initKey {
		initKey[i] = byte(i)
	}
	if err := os.WriteFile(keyPath, initKey[:], 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	cipher, err := secrets.NewCipher(initKey)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	store := managementstate.NewStore(statePath, cipher)
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen: "127.0.0.1:2096",
			Mode:        "server",
		},
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Run rotate-key command
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"admin", "rotate-key",
		"--state", statePath,
		"--key-path", keyPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing rotate-key: %v\nOutput: %s", err, out.String())
	}

	// Verify key file has changed
	newKeyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read key file: %v", err)
	}
	if bytes.Equal(newKeyBytes, initKey[:]) {
		t.Fatal("expected key to have changed, but it is identical")
	}

	// Verify we can load the state with the new key
	var newKey [secrets.KeySize]byte
	copy(newKey[:], newKeyBytes)

	newCipher, err := secrets.NewCipher(newKey)
	if err != nil {
		t.Fatalf("failed to create new cipher: %v", err)
	}

	newStore := managementstate.NewStore(statePath, newCipher)
	newSnapshot, ok, err := newStore.Load()
	if err != nil {
		t.Fatalf("failed to load state with new key: %v", err)
	}
	if !ok {
		t.Fatal("expected state snapshot to be present")
	}

	if newSnapshot.Settings.PanelListen != "127.0.0.1:2096" {
		t.Errorf("expected PanelListen to be '127.0.0.1:2096', got %q", newSnapshot.Settings.PanelListen)
	}
}

func TestAdminRotateKeyToNewPath(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	newKeyPath := filepath.Join(tempEtc, "new_state.key")

	var initKey [secrets.KeySize]byte
	for i := range initKey {
		initKey[i] = byte(i + 10)
	}
	if err := os.WriteFile(keyPath, initKey[:], 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	cipher, err := secrets.NewCipher(initKey)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	store := managementstate.NewStore(statePath, cipher)
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen: "127.0.0.1:3000",
			Mode:        "server",
		},
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Run rotate-key to a new path
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"admin", "rotate-key",
		"--state", statePath,
		"--key-path", keyPath,
		"--new-key-path", newKeyPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing rotate-key: %v\nOutput: %s", err, out.String())
	}

	// Original key file should be unchanged
	origKeyBytes, _ := os.ReadFile(keyPath)
	if !bytes.Equal(origKeyBytes, initKey[:]) {
		t.Fatal("expected original key file to remain unchanged")
	}

	// New key file should exist and be different
	newKeyBytes, err := os.ReadFile(newKeyPath)
	if err != nil {
		t.Fatalf("failed to read new key file: %v", err)
	}
	if bytes.Equal(newKeyBytes, initKey[:]) {
		t.Fatal("expected new key to be different from original")
	}

	// Verify state can be decrypted with the new key
	var newKey [secrets.KeySize]byte
	copy(newKey[:], newKeyBytes)
	newCipher, _ := secrets.NewCipher(newKey)
	newStore := managementstate.NewStore(statePath, newCipher)
	newSnapshot, ok, _ := newStore.Load()
	if !ok || newSnapshot.Settings.PanelListen != "127.0.0.1:3000" {
		t.Fatal("failed to decrypt state with new key file")
	}
}
