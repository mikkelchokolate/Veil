package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// The privileged helper restores state/key as root while the panel runs as an
// unprivileged user. The staged replacement must keep the original file's
// mode (and, as root, ownership) — otherwise the panel cannot re-read its own
// state after a restore and the reload step fails.
func TestStageRestoreFilePreservesOriginalMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.key")
	if err := os.WriteFile(target, []byte("old-key"), 0o640); err != nil {
		t.Fatalf("write original: %v", err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatalf("chmod original: %v", err)
	}

	staged, err := stageRestoreFile(target, []byte("new-key"), target+".safety")
	if err != nil {
		t.Fatalf("stageRestoreFile: %v", err)
	}
	info, err := os.Stat(staged.temp)
	if err != nil {
		t.Fatalf("stat staged: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("staged mode = %o, want 640 (original preserved)", got)
	}
	if err := staged.commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	final, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := final.Mode().Perm(); got != 0o640 {
		t.Fatalf("target mode after commit = %o, want 640", got)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(body) != "new-key" {
		t.Fatalf("target body = %q, want new-key", body)
	}
}

// Without an original file, the staged file falls back to the historical
// restrictive default (0600).
func TestStageRestoreFileDefaultsModeWithoutOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.key")

	staged, err := stageRestoreFile(target, []byte("new-key"), target+".safety")
	if err != nil {
		t.Fatalf("stageRestoreFile: %v", err)
	}
	info, err := os.Stat(staged.temp)
	if err != nil {
		t.Fatalf("stat staged: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("staged mode = %o, want 600 default", got)
	}
	_ = staged.rollback()
}
