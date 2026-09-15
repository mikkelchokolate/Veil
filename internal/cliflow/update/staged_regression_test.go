package update

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestartAfterUpdateHealthCheckUsesInstalledListen(t *testing.T) {
	t.Setenv("VEIL_LISTEN", "127.0.0.1:47359")
	var gotAddr string
	err := RestartAfterUpdate(io.Discard, "current", "backup", WorkflowOptions{Staged: true}, RestartHooks{
		Restart: func(string) error { return nil },
		Health: func(addr, token string, timeout time.Duration) error {
			gotAddr = addr
			return nil
		},
		Rollback: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("RestartAfterUpdate: %v", err)
	}
	if gotAddr != "127.0.0.1:47359" {
		t.Fatalf("health check addr = %q, want installed listen 127.0.0.1:47359", gotAddr)
	}
}

func TestRestartAfterUpdateDoesNotRollbackWhenPrefixedHealthzOK(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/secret-panel/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	rolledBack := false
	err := RestartAfterUpdate(io.Discard, "current", "backup", WorkflowOptions{
		Staged:      true,
		Listen:      server.URL,
		WebBasePath: "/secret-panel/",
	}, RestartHooks{
		Restart: func(string) error { return nil },
		Rollback: func(string, string) error {
			rolledBack = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RestartAfterUpdate: %v (path=%q)", err, gotPath)
	}
	if rolledBack {
		t.Fatal("staged update rolled back even though prefixed healthz returned 200")
	}
	if gotPath != "/secret-panel/healthz" {
		t.Fatalf("path = %q, want /secret-panel/healthz", gotPath)
	}
}

func copyBackupRollback(backupPath, currentPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}
	return os.WriteFile(currentPath, data, 0o755)
}

func TestRestartAfterUpdateStartsRestoredBinaryAfterStagedRestartFailure(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	if err := os.WriteFile(currentPath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	restarts := 0
	healthCalls := 0
	var out bytes.Buffer
	err := RestartAfterUpdate(&out, currentPath, backupPath, WorkflowOptions{Staged: true}, RestartHooks{
		Restart: func(string) error {
			restarts++
			if restarts == 1 {
				return fmt.Errorf("incompatible binary")
			}
			return nil
		},
		Health: func(string, string, time.Duration) error {
			healthCalls++
			return nil
		},
		Rollback: copyBackupRollback,
	})
	if err == nil || !strings.Contains(err.Error(), "restart failed, rolled back") {
		t.Fatalf("expected original restart failure after successful recovery, got %v", err)
	}
	if !strings.Contains(err.Error(), "incompatible binary") {
		t.Fatalf("expected original restart error to be preserved, got %v", err)
	}
	body, readErr := os.ReadFile(currentPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "old-binary" {
		t.Fatalf("rollback did not restore old binary: %q", string(body))
	}
	if restarts < 2 {
		t.Fatalf("restored binary was never started again: restart calls = %d", restarts)
	}
	if healthCalls < 1 {
		t.Fatalf("restored binary was never health-checked: health calls = %d", healthCalls)
	}
	if !strings.Contains(out.String(), "Rolled back to previous binary.") {
		t.Fatalf("successful recovery message missing:\n%s", out.String())
	}
}

func TestRestartAfterUpdateRestartsAfterStagedHealthCheckFailure(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	if err := os.WriteFile(currentPath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	restarts := 0
	healthCalls := 0
	var out bytes.Buffer
	err := RestartAfterUpdate(&out, currentPath, backupPath, WorkflowOptions{Staged: true}, RestartHooks{
		Restart: func(string) error {
			restarts++
			return nil
		},
		Health: func(string, string, time.Duration) error {
			healthCalls++
			if healthCalls == 1 {
				return fmt.Errorf("unhealthy")
			}
			return nil
		},
		Rollback: copyBackupRollback,
	})
	if err == nil || !strings.Contains(err.Error(), "health check failed, rolled back") {
		t.Fatalf("expected health check failure after successful recovery, got %v", err)
	}
	body, readErr := os.ReadFile(currentPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "old-binary" {
		t.Fatalf("rollback did not restore old binary: %q", string(body))
	}
	if restarts < 2 {
		t.Fatalf("restored binary was never started again: restart calls = %d", restarts)
	}
	if healthCalls < 2 {
		t.Fatalf("restored binary was never health-checked: health calls = %d", healthCalls)
	}
	if !strings.Contains(out.String(), "Rolled back to previous binary.") {
		t.Fatalf("successful recovery message missing:\n%s", out.String())
	}
}

func TestRestartAfterUpdateDoesNotClaimRecoveryWhenRestoredRestartFails(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	if err := os.WriteFile(currentPath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := RestartAfterUpdate(&out, currentPath, backupPath, WorkflowOptions{Staged: true}, RestartHooks{
		Restart:  func(string) error { return fmt.Errorf("systemctl failed") },
		Health:   func(string, string, time.Duration) error { return nil },
		Rollback: copyBackupRollback,
	})
	if err == nil {
		t.Fatal("expected combined restart and recovery error")
	}
	if !strings.Contains(err.Error(), "systemctl failed") {
		t.Fatalf("expected original restart error, got %v", err)
	}
	if !strings.Contains(err.Error(), "restored previous binary but restart failed") {
		t.Fatalf("expected recovery restart failure, got %v", err)
	}
	if strings.Contains(out.String(), "Rolled back to previous binary.") {
		t.Fatalf("must not claim recovery succeeded when restored binary was not started:\n%s", out.String())
	}
	body, readErr := os.ReadFile(currentPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "old-binary" {
		t.Fatalf("rollback did not restore old binary: %q", string(body))
	}
}
