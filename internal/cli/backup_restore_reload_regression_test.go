package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestCLIBackupRestoreRefusesDatabaseArchiveWhilePanelRuns locks #1452: a SIGHUP
// reload re-reads state.json but keeps the open SQLite handle to the old,
// unlinked veil.db, so the Panel would serve pre-restore clients and silently
// drop every write made after the "successful" reload. The CLI has to refuse
// before touching anything and tell the operator to stop the Panel.
func TestCLIBackupRestoreRefusesDatabaseArchiveWhilePanelRuns(t *testing.T) {
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
	backupSystemctlRun = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { backupSystemctlRun = oldRun })

	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	restore := NewRootCommand("test")
	out := &bytes.Buffer{}
	restore.SetOut(out)
	restore.SetErr(out)
	restore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--allow-unencrypted",
		"--yes",
	})
	err = restore.Execute()
	if err == nil {
		t.Fatalf("restore of a database archive succeeded while veil.service was active:\n%s", out.String())
	}
	for _, want := range []string{"refusing to restore while veil.service is running", "systemctl stop veil.service"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
	// Only the activity probe may run: no HUP, and nothing was restored.
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], []string{"is-active", "--quiet", "veil.service"}) {
		t.Fatalf("systemctl calls = %#v, want one is-active probe", calls)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("state changed although the restore was refused")
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
	backupSystemctlRun = func(args ...string) error {
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
		"--allow-unencrypted",
		"--yes",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("backup restore: %v\n%s", err, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("Signaled the running Panel")) {
		t.Fatalf("inactive Panel should not be signaled:\n%s", out)
	}
	for _, call := range calls {
		if call[0] != "is-active" {
			t.Fatalf("inactive Panel must never be signalled: %#v", calls)
		}
	}
}
