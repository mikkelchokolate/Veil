package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupCreateRequiresEncryptionOrExplicitOverride(t *testing.T) {
	statePath, keyPath := writeCLIBackupSource(t)
	command := NewRootCommand("0.6.0")
	command.SetOut(new(bytes.Buffer))
	command.SetErr(new(bytes.Buffer))
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", filepath.Join(t.TempDir(), "backup.tar.gz"),
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "--allow-unencrypted") {
		t.Fatalf("expected encryption refusal, got %v", err)
	}
}

func TestBackupCreateOutputDirAndPruneCLI(t *testing.T) {
	statePath, keyPath := writeCLIBackupSource(t)
	outputDir := t.TempDir()
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("scheduled-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{
		"veil_backup_20240101_020000.tar.gz.enc",
		"veil_backup_20240201_020000.tar.gz.enc",
	} {
		if err := os.WriteFile(filepath.Join(outputDir, old), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	command := NewRootCommand("0.6.0")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--output-dir", outputDir,
		"--prune",
		"--daily", "1",
		"--weekly", "0",
		"--monthly", "0",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("create: %v output=%s", err, output.String())
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".enc") {
		t.Fatalf("retained entries=%v output=%s", entries, output.String())
	}
}

func TestBackupListAndPruneCLI(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"veil_backup_20260605_020000.tar.gz.enc",
		"veil_backup_20260604_020000.tar.gz.enc",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list := NewRootCommand("0.6.0")
	var listOutput bytes.Buffer
	list.SetOut(&listOutput)
	list.SetErr(&listOutput)
	list.SetArgs([]string{"backup", "list", "--dir", dir})
	if err := list.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOutput.String(), "20260605") {
		t.Fatalf("list output=%s", listOutput.String())
	}

	prune := NewRootCommand("0.6.0")
	prune.SetOut(new(bytes.Buffer))
	prune.SetErr(new(bytes.Buffer))
	prune.SetArgs([]string{
		"backup", "prune",
		"--dir", dir,
		"--daily", "1",
		"--weekly", "0",
		"--monthly", "0",
	})
	if err := prune.Execute(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "veil_backup_20260605_020000.tar.gz.enc" {
		t.Fatalf("entries after prune=%v", entries)
	}
}

func TestBackupScheduleEnableWritesProtectedPassphraseAndEnablesTimer(t *testing.T) {
	target := filepath.Join(t.TempDir(), "backup.passphrase")
	oldRunner := backupSystemctlRun
	t.Cleanup(func() { backupSystemctlRun = oldRunner })
	var calls [][]string
	backupSystemctlRun = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	command := NewRootCommand("0.6.0")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"backup", "schedule", "enable",
		"--passphrase", "a-long-scheduled-backup-passphrase",
		"--passphrase-path", target,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("schedule enable: %v output=%s", err, output.String())
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "a-long-scheduled-backup-passphrase\n" {
		t.Fatalf("passphrase file=%q", body)
	}
	if len(calls) != 2 ||
		strings.Join(calls[0], " ") != "daemon-reload" ||
		strings.Join(calls[1], " ") != "enable --now veil-backup.timer" {
		t.Fatalf("systemctl calls=%v", calls)
	}
}

func TestBackupScheduleDisableStopsTimerAndCanRemovePassphrase(t *testing.T) {
	target := filepath.Join(t.TempDir(), "backup.passphrase")
	if err := os.WriteFile(target, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRunner := backupSystemctlRun
	t.Cleanup(func() { backupSystemctlRun = oldRunner })
	var calls [][]string
	backupSystemctlRun = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	command := NewRootCommand("0.6.0")
	command.SetOut(new(bytes.Buffer))
	command.SetErr(new(bytes.Buffer))
	command.SetArgs([]string{
		"backup", "schedule", "disable",
		"--passphrase-path", target,
		"--remove-passphrase",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "disable --now veil-backup.timer" {
		t.Fatalf("systemctl calls=%v", calls)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("passphrase file remains: %v", err)
	}
}
