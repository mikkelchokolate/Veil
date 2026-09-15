package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScheduledPassphrasePathReadsDropInExecStart(t *testing.T) {
	dir := t.TempDir()
	dropInDir := filepath.Join(dir, ScheduleDropInDir)
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[Service]\nExecStart=/usr/local/bin/veil backup create --passphrase-file /etc/veil/custom.passphrase --prune\n"
	if err := os.WriteFile(filepath.Join(dropInDir, ScheduleDropInName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ScheduledPassphrasePath(dir)
	want := filepath.Clean(filepath.FromSlash("/etc/veil/custom.passphrase"))
	if got != want {
		t.Fatalf("ScheduledPassphrasePath = %q, want %q", got, want)
	}
}

func TestScheduledPassphrasePathMissingDropIn(t *testing.T) {
	if got := ScheduledPassphrasePath(t.TempDir()); got != "" {
		t.Fatalf("empty systemd dir should yield no path, got %q", got)
	}
}
