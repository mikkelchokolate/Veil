package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndRestoreBackupUnencrypted(t *testing.T) {
	dir := t.TempDir()

	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")

	stateContent := []byte(`{"version": 1, "data": "test"}`)
	keyContent := bytes.Repeat([]byte{0x42}, 32)

	if err := os.WriteFile(statePath, stateContent, 0o600); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	// Create backup
	backupData, err := CreateBackup(statePath, keyPath, "")
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Restore backup to new paths
	newStatePath := filepath.Join(dir, "new_state.json")
	newKeyPath := filepath.Join(dir, "new_state.key")

	err = RestoreBackup(backupData, newStatePath, newKeyPath, "")
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	// Verify content
	restoredState, err := os.ReadFile(newStatePath)
	if err != nil {
		t.Fatalf("failed to read restored state: %v", err)
	}
	if !bytes.Equal(restoredState, stateContent) {
		t.Errorf("expected state %q, got %q", stateContent, restoredState)
	}

	restoredKey, err := os.ReadFile(newKeyPath)
	if err != nil {
		t.Fatalf("failed to read restored key: %v", err)
	}
	if !bytes.Equal(restoredKey, keyContent) {
		t.Errorf("expected key %v, got %v", keyContent, restoredKey)
	}
}

func TestCreateAndRestoreBackupEncrypted(t *testing.T) {
	dir := t.TempDir()

	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")

	stateContent := []byte(`{"secure": true}`)
	keyContent := bytes.Repeat([]byte{0x99}, 32)

	if err := os.WriteFile(statePath, stateContent, 0o600); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	passphrase := "my-secure-passphrase"

	// Create encrypted backup
	backupData, err := CreateBackup(statePath, keyPath, passphrase)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Try to restore with missing passphrase
	newStatePath := filepath.Join(dir, "new_state.json")
	newKeyPath := filepath.Join(dir, "new_state.key")

	err = RestoreBackup(backupData, newStatePath, newKeyPath, "")
	if err == nil {
		t.Fatal("expected error when restoring encrypted backup with empty passphrase")
	}

	// Try to restore with wrong passphrase
	err = RestoreBackup(backupData, newStatePath, newKeyPath, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error when restoring encrypted backup with wrong passphrase")
	}

	// Restore with correct passphrase
	err = RestoreBackup(backupData, newStatePath, newKeyPath, passphrase)
	if err != nil {
		t.Fatalf("RestoreBackup failed with correct passphrase: %v", err)
	}

	// Verify content
	restoredState, err := os.ReadFile(newStatePath)
	if err != nil {
		t.Fatalf("failed to read restored state: %v", err)
	}
	if !bytes.Equal(restoredState, stateContent) {
		t.Errorf("expected state %q, got %q", stateContent, restoredState)
	}

	restoredKey, err := os.ReadFile(newKeyPath)
	if err != nil {
		t.Fatalf("failed to read restored key: %v", err)
	}
	if !bytes.Equal(restoredKey, keyContent) {
		t.Errorf("expected key %v, got %v", keyContent, restoredKey)
	}
}

func TestRestoreUnencryptedBackupWithPassphraseErrors(t *testing.T) {
	dir := t.TempDir()

	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")

	stateContent := []byte(`{"secure": false}`)
	keyContent := bytes.Repeat([]byte{0x11}, 32)

	if err := os.WriteFile(statePath, stateContent, 0o600); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	// Unencrypted backup
	backupData, err := CreateBackup(statePath, keyPath, "")
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Try to restore unencrypted backup with a passphrase
	err = RestoreBackup(backupData, filepath.Join(dir, "ns.json"), filepath.Join(dir, "nk.key"), "pass")
	if err == nil {
		t.Fatal("expected error when trying to decrypt unencrypted backup")
	}
}
