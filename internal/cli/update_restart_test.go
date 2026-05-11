package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
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

	oldRestart := runSystemctlRestart
	runSystemctlRestart = func(unit string) error { return fmt.Errorf("restart failed") }
	t.Cleanup(func() { runSystemctlRestart = oldRestart })

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
	if !strings.Contains(out.String(), "Rolled back to previous binary.") {
		t.Fatalf("rollback output missing:\n%s", out.String())
	}
}
