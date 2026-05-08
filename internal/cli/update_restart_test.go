package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitForHealthySupportsGeneratedPanelTLSOnLoopback(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := waitForHealthy(server.URL, "", time.Second); err != nil {
		t.Fatalf("waitForHealthy should trust generated Panel TLS on loopback: %v", err)
	}
}

func TestRestartUpdatedVeilRollsBackWhenStagedRestartFails(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	if err := os.WriteFile(currentPath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldRestart := runSystemctlRestart
	runSystemctlRestart = func(unit string) error { return fmt.Errorf("restart failed") }
	t.Cleanup(func() { runSystemctlRestart = oldRestart })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := restartUpdatedVeil(cmd, currentPath, backupPath, updateWorkflowOptions{Staged: true})
	if err == nil || !strings.Contains(err.Error(), "restart failed, rolled back") {
		t.Fatalf("expected staged rollback error, got %v", err)
	}
	body, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old-binary" {
		t.Fatalf("rollback did not restore old binary: %q", string(body))
	}
	if !strings.Contains(out.String(), "Rolled back to previous binary.") {
		t.Fatalf("rollback output missing:\n%s", out.String())
	}
}
