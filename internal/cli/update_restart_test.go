package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
)

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

	restarts := 0
	oldRestart := runSystemctlRestart
	oldHealth := updateHealthChecker
	runSystemctlRestart = func(unit string) error {
		restarts++
		if restarts == 1 {
			return fmt.Errorf("restart failed")
		}
		return nil
	}
	updateHealthChecker = func(string, string, time.Duration) error { return nil }
	t.Cleanup(func() {
		runSystemctlRestart = oldRestart
		updateHealthChecker = oldHealth
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := restartUpdatedVeil(cmd, currentPath, backupPath, updateflow.WorkflowOptions{Staged: true})
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
	if restarts < 2 {
		t.Fatalf("restored binary was never started again: restart calls = %d", restarts)
	}
	if !strings.Contains(out.String(), "Rolled back to previous binary.") {
		t.Fatalf("rollback output missing:\n%s", out.String())
	}
}
