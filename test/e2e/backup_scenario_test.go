//go:build e2e

package e2e

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedBackupRestoreWorkflow(t *testing.T) {
	srv := startServer(t, serverOptions{token: "backup-e2e-token"})
	response := srv.do(
		http.MethodPut,
		"/api/settings",
		`{"panelListen":"127.0.0.1:2096","panelAccess":"local","mode":"server","domain":"backup.example.com"}`,
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", response.StatusCode, readJSON(t, response))
	}
	drain(response)
	srv.stop()

	statePath := srv.statePath
	keyPath := filepath.Join(filepath.Dir(statePath), "state.key")
	originalState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	originalKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(t.TempDir(), "veil-backup.tar.gz.enc")
	const passphrase = "backup-e2e-passphrase"
	createOut, err := runCLI(t, nil,
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase", passphrase,
		"--output", archivePath,
	)
	if err != nil {
		t.Fatalf("backup create failed: %v\n%s", err, createOut)
	}

	verifyOut, err := runCLI(t, nil,
		"backup", "verify", archivePath,
		"--passphrase", passphrase,
	)
	if err != nil || !strings.Contains(verifyOut, "Backup verified.") {
		t.Fatalf("backup verify failed: %v\n%s", err, verifyOut)
	}

	checkOut, err := runCLI(t, nil,
		"backup", "restore", archivePath,
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase", passphrase,
		"--check-only",
	)
	if err != nil || !strings.Contains(checkOut, "Restore check passed") {
		t.Fatalf("backup restore check failed: %v\n%s", err, checkOut)
	}

	if err := os.WriteFile(statePath, []byte("corrupted state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x22}, len(originalKey)), 0o600); err != nil {
		t.Fatal(err)
	}

	restoreOut, err := runCLI(t, nil,
		"backup", "restore", archivePath,
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase", passphrase,
		"--yes",
	)
	if err != nil {
		t.Fatalf("backup restore failed: %v\n%s", err, restoreOut)
	}
	restoredState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	restoredKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredState, originalState) || !bytes.Equal(restoredKey, originalKey) {
		t.Fatal("restored state/key do not match the archived bytes")
	}
}
