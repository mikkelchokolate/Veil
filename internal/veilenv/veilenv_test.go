package veilenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareKeyPathKeepsEveryOtherLineAndTheFileMode(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "veil.env")
	original := "VEIL_LISTEN=127.0.0.1:2096\nVEIL_KEY_PATH=/etc/veil/state.key\nVEIL_WEB_BASE_PATH=/abc/\n"
	if err := os.WriteFile(envPath, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}

	update, err := PrepareKeyPath(envPath, "/etc/veil/rotated-state.key")
	if err != nil {
		t.Fatalf("PrepareKeyPath: %v", err)
	}
	if update.Path() != envPath || update.KeyPath() != "/etc/veil/rotated-state.key" {
		t.Fatalf("update = %s -> %s", update.Path(), update.KeyPath())
	}
	// Nothing is visible to the units before Commit: the live file still names
	// the old key, which is what keeps the staged rewrite safe.
	if body, err := os.ReadFile(envPath); err != nil || string(body) != original {
		t.Fatalf("live environment changed before Commit: %q err=%v", body, err)
	}

	if err := update.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "VEIL_LISTEN=127.0.0.1:2096\nVEIL_KEY_PATH=/etc/veil/rotated-state.key\nVEIL_WEB_BASE_PATH=/abc/\n"
	if string(body) != want {
		t.Fatalf("rewritten environment =\n%q\nwant\n%q", body, want)
	}
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode().Perm())
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("staged file left behind: %v entries=%d", err, len(entries))
	}
}

func TestPrepareKeyPathAppendsTheKeyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_LISTEN=127.0.0.1:2096\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	update, err := PrepareKeyPath(envPath, "/etc/veil/new.key")
	if err != nil {
		t.Fatalf("PrepareKeyPath: %v", err)
	}
	if err := update.Commit(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if Value(body, KeyPathKey) != "/etc/veil/new.key" {
		t.Fatalf("VEIL_KEY_PATH = %q in\n%s", Value(body, KeyPathKey), body)
	}
	if !strings.Contains(string(body), "VEIL_LISTEN=127.0.0.1:2096\n") {
		t.Fatalf("unrelated line lost:\n%s", body)
	}
}

func TestPrepareKeyPathRefusesAMissingFileAndLeavesNothingStaged(t *testing.T) {
	dir := t.TempDir()
	if _, err := PrepareKeyPath(filepath.Join(dir, "veil.env"), "/etc/veil/new.key"); err == nil {
		t.Fatal("PrepareKeyPath succeeded without an environment file")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("staged files after a refusal: %v entries=%d", err, len(entries))
	}
}

func TestPreparedUpdateAbortDropsTheStagedCopy(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "veil.env")
	original := "VEIL_KEY_PATH=/etc/veil/state.key\n"
	if err := os.WriteFile(envPath, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	update, err := PrepareKeyPath(envPath, "/etc/veil/new.key")
	if err != nil {
		t.Fatal(err)
	}
	update.Abort()
	update.Abort()
	if body, err := os.ReadFile(envPath); err != nil || string(body) != original {
		t.Fatalf("environment changed by Abort: %q err=%v", body, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("staged file survived Abort: %v entries=%d", err, len(entries))
	}
}
