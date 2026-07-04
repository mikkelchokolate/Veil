package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

func TestWriteBackupArchiveCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.enc")
	body := []byte("backup body")

	if err := writeBackupArchive(path, body); err != nil {
		t.Fatalf("writeBackupArchive: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q, want %q", got, body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteBackupArchiveReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.enc")
	_ = os.WriteFile(path, []byte("old"), 0o600)

	if err := writeBackupArchive(path, []byte("new")); err != nil {
		t.Fatalf("writeBackupArchive: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("got %q, want new", got)
	}
}

func TestWriteBackupArchiveFailsWhenDirectoryIsAFile(t *testing.T) {
	dir := t.TempDir()
	badParent := filepath.Join(dir, "notadir")
	_ = os.WriteFile(badParent, []byte("x"), 0o600)
	path := filepath.Join(badParent, "archive.enc")

	if err := writeBackupArchive(path, []byte("body")); err == nil {
		t.Fatal("expected error when parent path is a file")
	}
}

func TestResolvePassphraseMutuallyExclusive(t *testing.T) {
	_, err := resolvePassphrase("foo", "bar")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestResolvePassphraseReadsFileAndTrimsNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pass.txt")
	if err := os.WriteFile(path, []byte("secret-from-file\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePassphrase("", path)
	if err != nil {
		t.Fatalf("resolvePassphrase: %v", err)
	}
	if got != "secret-from-file" {
		t.Fatalf("got %q, want secret-from-file", got)
	}
}

func TestResolvePassphraseReturnsErrorForMissingFile(t *testing.T) {
	_, err := resolvePassphrase("", filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "read passphrase file") {
		t.Fatalf("expected passphrase file error, got %v", err)
	}
}

func TestResolvePassphraseReturnsEmptyWhenNotInteractive(t *testing.T) {
	// Redirect stdin to /dev/null so the terminal check returns false.
	oldStdin := os.Stdin
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = f.Close()
	})

	got, err := resolvePassphrase("", "")
	if err != nil {
		t.Fatalf("resolvePassphrase: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestBackupScheduleEnableAndDisable(t *testing.T) {
	var calls [][]string
	old := backupSystemctlRun
	backupSystemctlRun = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { backupSystemctlRun = old })

	dir := t.TempDir()
	passPath := filepath.Join(dir, "backup.passphrase")
	passphrase := "sixteen-char-phrase"

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"backup", "schedule", "enable",
		"--passphrase", passphrase,
		"--passphrase-path", passPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("schedule enable: %v\n%s", err, out.String())
	}

	stored, err := os.ReadFile(passPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != passphrase+"\n" {
		t.Fatalf("stored passphrase = %q", stored)
	}

	cmd2 := NewRootCommand("test")
	cmd2.SetOut(io.Discard)
	cmd2.SetErr(io.Discard)
	cmd2.SetArgs([]string{
		"backup", "schedule", "disable",
		"--passphrase-path", passPath,
		"--remove-passphrase",
	})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("schedule disable: %v", err)
	}

	if _, err := os.Stat(passPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected passphrase file to be removed, got err=%v", err)
	}

	want := [][]string{
		{"daemon-reload"},
		{"enable", "--now", "veil-backup.timer"},
		{"disable", "--now", "veil-backup.timer"},
	}
	if len(calls) != len(want) {
		t.Fatalf("systemctl calls = %v", calls)
	}
	for i := range want {
		if len(calls[i]) != len(want[i]) {
			t.Fatalf("call %d = %v, want %v", i, calls[i], want[i])
		}
		for j := range want[i] {
			if calls[i][j] != want[i][j] {
				t.Fatalf("call %d = %v, want %v", i, calls[i], want[i])
			}
		}
	}
}

func TestBackupScheduleEnableRejectsShortPassphrase(t *testing.T) {
	cmd := NewRootCommand("test")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"backup", "schedule", "enable",
		"--passphrase", "short",
	})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "at least 16 characters") {
		t.Fatalf("expected short passphrase error, got %v", err)
	}
}

func TestBackupScheduleDisableReportsSystemctlError(t *testing.T) {
	old := backupSystemctlRun
	backupSystemctlRun = func(args ...string) error { return fmt.Errorf("systemctl error") }
	t.Cleanup(func() { backupSystemctlRun = old })

	cmd := NewRootCommand("test")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"backup", "schedule", "disable"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "disable veil-backup.timer") {
		t.Fatalf("expected systemctl error, got %v", err)
	}
}

func TestPrintBackupVerificationLegacy(t *testing.T) {
	cmd := newBackupCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	printBackupVerification(cmd, backup.VerificationReport{
		FormatVersion: 1,
		Legacy:        true,
		Encrypted:     false,
		Files:         []backup.ArchiveFile{{Name: "state.json", Size: 10, SHA256: "abcd"}},
	})
	got := out.String()
	if !strings.Contains(got, "Archive format: legacy") {
		t.Fatalf("missing legacy format:\n%s", got)
	}
}

func TestPrintBackupVerificationOmitsEmptyVersion(t *testing.T) {
	cmd := newBackupCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	printBackupVerification(cmd, backup.VerificationReport{
		FormatVersion: 2,
		Encrypted:     true,
		Files:         []backup.ArchiveFile{},
	})
	got := out.String()
	if strings.Contains(got, "Veil version") {
		t.Fatalf("should not print empty version:\n%s", got)
	}
}
