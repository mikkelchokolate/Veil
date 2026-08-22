package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

func TestBackupVerifyAndRestoreCheckOnlyCLI(t *testing.T) {
	statePath, keyPath := writeCLIBackupSource(t)
	backupPath := filepath.Join(t.TempDir(), "state.enc")
	data, err := backup.CreateBackupWithOptions(statePath, keyPath, "passphrase", backup.ArchiveOptions{
		VeilVersion: "0.5.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	verify := NewRootCommand("0.6.0")
	var verifyOut bytes.Buffer
	verify.SetOut(&verifyOut)
	verify.SetErr(&verifyOut)
	verify.SetArgs([]string{"backup", "verify", backupPath, "--passphrase", "passphrase"})
	if err := verify.Execute(); err != nil {
		t.Fatalf("verify: %v output=%s", err, verifyOut.String())
	}
	if !strings.Contains(verifyOut.String(), "Backup verified") ||
		!strings.Contains(verifyOut.String(), "Veil version: 0.5.0") {
		t.Fatalf("verify output=%s", verifyOut.String())
	}

	targetDir := t.TempDir()
	targetState := filepath.Join(targetDir, "state.json")
	targetKey := filepath.Join(targetDir, "state.key")
	restore := NewRootCommand("0.6.0")
	var restoreOut bytes.Buffer
	restore.SetOut(&restoreOut)
	restore.SetErr(&restoreOut)
	restore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--passphrase", "passphrase",
		"--state", targetState,
		"--key-path", targetKey,
		"--check-only",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("check-only restore: %v output=%s", err, restoreOut.String())
	}
	if !strings.Contains(restoreOut.String(), "Restore check passed") {
		t.Fatalf("restore output=%s", restoreOut.String())
	}
	if _, err := os.Stat(targetState); !os.IsNotExist(err) {
		t.Fatalf("check-only wrote state: %v", err)
	}
}

func createCLIBackupDatabase(t *testing.T, statePath string) {
	t.Helper()
	db := testdb.CloneTo(t, filepath.Join(filepath.Dir(statePath), "veil.db"))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeCLIBackupSource(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	state := `{"schemaVersion":3,"settings":{"panelListen":"127.0.0.1:2096","panelAccess":"local","mode":"server"}}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x4c}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	createCLIBackupDatabase(t, statePath)
	return statePath, keyPath
}
