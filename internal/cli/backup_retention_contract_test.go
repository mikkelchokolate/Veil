package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Regression tests for #1412: the CLI must give a retention triple the same
// meaning as the API and the privileged helper. All-zero means the default
// policy (not "keep nothing"), and out-of-range counts are rejected.

func writeRetentionArchives(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func retentionDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestBackupPruneAllZeroUsesDefaultPolicy(t *testing.T) {
	archives := []string{
		"veil_backup_20260901_120000.tar.gz.enc",
		"veil_backup_20261022_120000.tar.gz.enc",
		"veil_backup_20261023_120000.tar.gz.enc",
	}
	for _, dryRun := range []bool{true, false} {
		dir := t.TempDir()
		writeRetentionArchives(t, dir, archives...)
		command := NewRootCommand("0.6.0")
		var stdout, stderr bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(&stderr)
		args := []string{"backup", "prune", "--dir", dir, "--daily", "0", "--weekly", "0", "--monthly", "0"}
		if dryRun {
			args = append(args, "--dry-run")
		}
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("dry-run=%t prune: %v stderr=%s", dryRun, err, stderr.String())
		}
		if got := retentionDirNames(t, dir); strings.Join(got, ",") != strings.Join(archives, ",") {
			t.Fatalf("dry-run=%t: all-zero prune deleted archives, left %v", dryRun, got)
		}
		if strings.Contains(stdout.String(), "elete:") {
			t.Fatalf("dry-run=%t: all-zero prune reported deletions: %s", dryRun, stdout.String())
		}
		if !strings.Contains(stdout.String(), "Retention: kept 3, deleted 0") {
			t.Fatalf("dry-run=%t: stdout=%s", dryRun, stdout.String())
		}
		if !strings.Contains(stderr.String(), "using the default retention (daily 7, weekly 4, monthly 12)") {
			t.Fatalf("dry-run=%t: missing default-policy note, stderr=%s", dryRun, stderr.String())
		}
	}
}

func TestBackupPruneRejectsOutOfRangeRetention(t *testing.T) {
	for _, tc := range []struct {
		flag, value, want string
	}{
		{"--daily", "366", "daily retention must be between 0 and 365"},
		{"--weekly", "105", "weekly retention must be between 0 and 104"},
		{"--monthly", "121", "monthly retention must be between 0 and 120"},
		{"--daily", "-1", "daily retention must be between 0 and 365"},
	} {
		dir := t.TempDir()
		writeRetentionArchives(t, dir,
			"veil_backup_20260604_020000.tar.gz.enc",
			"veil_backup_20260605_020000.tar.gz.enc",
		)
		command := NewRootCommand("0.6.0")
		command.SetOut(new(bytes.Buffer))
		command.SetErr(new(bytes.Buffer))
		command.SetArgs([]string{"backup", "prune", "--dir", dir, "--daily", "0", "--weekly", "0", "--monthly", "0", tc.flag, tc.value})
		err := command.Execute()
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%s %s: err=%v, want %q", tc.flag, tc.value, err, tc.want)
		}
		if got := retentionDirNames(t, dir); len(got) != 2 {
			t.Fatalf("%s %s: rejected prune still deleted archives, left %v", tc.flag, tc.value, got)
		}
	}
}

func TestBackupCreatePruneAllZeroKeepsNewArchive(t *testing.T) {
	statePath, keyPath := writeCLIBackupSource(t)
	outputDir := t.TempDir()
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("scheduled-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := NewRootCommand("0.6.0")
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--output-dir", outputDir,
		"--prune",
		"--daily", "0",
		"--weekly", "0",
		"--monthly", "0",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("create: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	got := retentionDirNames(t, outputDir)
	if len(got) != 1 || !strings.HasPrefix(got[0], "veil_backup_") || !strings.HasSuffix(got[0], ".enc") {
		t.Fatalf("create --prune with all-zero retention left %v; stdout=%s", got, stdout.String())
	}
	if !strings.Contains(stdout.String(), "Retention: kept 1, deleted 0") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestBackupCreatePruneRejectsOutOfRangeBeforeWriting(t *testing.T) {
	statePath, keyPath := writeCLIBackupSource(t)
	outputDir := t.TempDir()
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("scheduled-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := NewRootCommand("0.6.0")
	command.SetOut(new(bytes.Buffer))
	command.SetErr(new(bytes.Buffer))
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--output-dir", outputDir,
		"--prune",
		"--weekly", "105",
	})
	err := command.Execute()
	if err == nil || err.Error() != "weekly retention must be between 0 and 104" {
		t.Fatalf("err=%v", err)
	}
	if got := retentionDirNames(t, outputDir); len(got) != 0 {
		t.Fatalf("rejected create --prune still wrote %v", got)
	}
}
