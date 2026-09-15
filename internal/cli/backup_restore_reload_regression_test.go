package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCLIBackupRestoreSignalsRunningPanel(t *testing.T) {
	tempVar := t.TempDir()
	tempEtc := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.tar.gz")
	if err := os.WriteFile(statePath, []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0xab}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	createCLIBackupDatabase(t, statePath)

	create := NewRootCommand("test")
	create.SetOut(&bytes.Buffer{})
	create.SetErr(&bytes.Buffer{})
	create.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--allow-unencrypted",
	})
	if err := create.Execute(); err != nil {
		t.Fatalf("backup create: %v", err)
	}

	var calls [][]string
	oldRun := backupSystemctlRun
	backupSystemctlRun = func(args []string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { backupSystemctlRun = oldRun })

	restore := NewRootCommand("test")
	out := &bytes.Buffer{}
	restore.SetOut(out)
	restore.SetErr(out)
	restore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--yes",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("backup restore: %v\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("Signaled the running Panel to reload restored state.")) {
		t.Fatalf("restore output missing reload notice:\n%s", out)
	}
	want := [][]string{
		{"is-active", "--quiet", "veil.service"},
		{"kill", "-s", "HUP", "veil.service"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, want)
	}
}

func TestCLIBackupRestoreSkipsReloadWhenPanelInactive(t *testing.T) {
	tempVar := t.TempDir()
	tempEtc := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.tar.gz")
	if err := os.WriteFile(statePath, []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0xab}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	createCLIBackupDatabase(t, statePath)

	create := NewRootCommand("test")
	create.SetOut(&bytes.Buffer{})
	create.SetErr(&bytes.Buffer{})
	create.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--allow-unencrypted",
	})
	if err := create.Execute(); err != nil {
		t.Fatalf("backup create: %v", err)
	}

	var calls [][]string
	oldRun := backupSystemctlRun
	backupSystemctlRun = func(args []string) error {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "is-active" {
			return os.ErrNotExist
		}
		t.Fatal("kill must not run when veil.service is inactive")
		return nil
	}
	t.Cleanup(func() { backupSystemctlRun = oldRun })

	restore := NewRootCommand("test")
	out := &bytes.Buffer{}
	restore.SetOut(out)
	restore.SetErr(out)
	restore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--yes",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("backup restore: %v\n%s", err, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("Signaled the running Panel")) {
		t.Fatalf("inactive Panel should not be signaled:\n%s", out)
	}
	if len(calls) != 1 || calls[0][0] != "is-active" {
		t.Fatalf("systemctl calls = %#v", calls)
	}
}
