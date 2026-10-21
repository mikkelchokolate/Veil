package service

import (
	"os"
	"path/filepath"
	"testing"
)

// #1262: systemd actions run during root install must not exec a PATH shim.
func TestSystemdExecRunnerIgnoresAmbientPATHShims(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(filepath.Join(dir, "veil-systemd-shim-1262"), []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (SystemdExecRunner{}).Run("veil-systemd-shim-1262", "daemon-reload"); err == nil {
		t.Fatal("expected not-found error")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("PATH shim was executed")
	}
}
