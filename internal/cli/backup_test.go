package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRestoreCLIUnencrypted(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.tar.gz")

	stateContent := []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`)
	keyContent := bytes.Repeat([]byte{0xab}, 32)

	if err := os.WriteFile(statePath, stateContent, 0o600); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	// 1. Create backup
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--allow-unencrypted",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("failed to run backup create: %v\nOutput: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "Backup successfully created:") {
		t.Fatalf("expected success message, got: %s", out.String())
	}

	// Verify backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file not created: %v", err)
	}

	// Delete original files
	if err := os.Remove(statePath); err != nil {
		t.Fatalf("failed to remove original state: %v", err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("failed to remove original key: %v", err)
	}

	// 2. Restore backup
	cmdRestore := NewRootCommand("test")
	var outRestore bytes.Buffer
	cmdRestore.SetOut(&outRestore)
	cmdRestore.SetErr(&outRestore)
	cmdRestore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--yes",
	})

	if err := cmdRestore.Execute(); err != nil {
		t.Fatalf("failed to run backup restore: %v\nOutput: %s", err, outRestore.String())
	}

	if !strings.Contains(outRestore.String(), "Backup successfully restored.") {
		t.Fatalf("expected success message, got: %s", outRestore.String())
	}

	// Verify restored contents
	restoredState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read restored state: %v", err)
	}
	if !bytes.Equal(restoredState, stateContent) {
		t.Errorf("expected state %s, got %s", stateContent, restoredState)
	}

	restoredKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read restored key: %v", err)
	}
	if !bytes.Equal(restoredKey, keyContent) {
		t.Errorf("expected key %v, got %v", keyContent, restoredKey)
	}
}

func TestBackupRestoreCLIEncrypted(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.enc")
	passphrase := "secret123"

	stateContent := []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`)
	keyContent := bytes.Repeat([]byte{0xcd}, 32)

	if err := os.WriteFile(statePath, stateContent, 0o600); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	// 1. Create backup with passphrase
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--passphrase", passphrase,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("failed to run backup create: %v\nOutput: %s", err, out.String())
	}

	// 2. Restore backup with wrong passphrase should fail
	cmdRestoreWrong := NewRootCommand("test")
	cmdRestoreWrong.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase", "wrongpass",
		"--yes",
	})
	if err := cmdRestoreWrong.Execute(); err == nil {
		t.Fatal("expected restore with wrong passphrase to fail")
	}

	// Remove files before restore
	os.Remove(statePath)
	os.Remove(keyPath)

	// 3. Restore backup with correct passphrase
	cmdRestoreCorrect := NewRootCommand("test")
	var outRestore bytes.Buffer
	cmdRestoreCorrect.SetOut(&outRestore)
	cmdRestoreCorrect.SetErr(&outRestore)
	cmdRestoreCorrect.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase", passphrase,
		"--yes",
	})

	if err := cmdRestoreCorrect.Execute(); err != nil {
		t.Fatalf("failed to run backup restore: %v\nOutput: %s", err, outRestore.String())
	}

	// Verify restored files
	restoredState, _ := os.ReadFile(statePath)
	if !bytes.Equal(restoredState, stateContent) {
		t.Errorf("state mismatch after restore")
	}
}

func TestBackupRestoreCLIPassphraseFile(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.enc")
	passphraseFile := filepath.Join(tempVar, "pass.txt")
	passphrase := "secret-from-file\r\n" // tests trim too

	if err := os.WriteFile(passphraseFile, []byte(passphrase), 0o600); err != nil {
		t.Fatalf("failed to write passphrase file: %v", err)
	}

	stateContent := []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`)
	keyContent := bytes.Repeat([]byte{0xef}, 32)

	if err := os.WriteFile(statePath, stateContent, 0o600); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}
	if err := os.WriteFile(keyPath, keyContent, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	// 1. Create backup with passphrase file
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--passphrase-file", passphraseFile,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("failed to run backup create: %v\nOutput: %s", err, out.String())
	}

	os.Remove(statePath)
	os.Remove(keyPath)

	// 2. Restore backup with passphrase file
	cmdRestore := NewRootCommand("test")
	cmdRestore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--yes",
	})

	if err := cmdRestore.Execute(); err != nil {
		t.Fatalf("failed to run backup restore: %v", err)
	}

	// Verify restored files
	restoredState, _ := os.ReadFile(statePath)
	if !bytes.Equal(restoredState, stateContent) {
		t.Errorf("state mismatch after restore")
	}
}

func TestBackupRestoreMutuallyExclusiveFlags(t *testing.T) {
	cmd := NewRootCommand("test")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"backup", "create",
		"--passphrase", "foo",
		"--passphrase-file", "bar",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error with mutually exclusive flags, but got nil")
	}
}
