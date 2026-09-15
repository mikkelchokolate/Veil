//go:build linux && linuxintegration

package linuxintegration

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBackupServiceHardeningCanReadVeilOwnedState(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("backup DAC probe requires root")
	}
	if _, err := os.Stat("/run/systemd/private"); err != nil {
		t.Skip("systemd manager is unavailable")
	}
	account, err := user.Lookup("veil")
	if err != nil {
		t.Skipf("veil account unavailable: %v", err)
	}
	uid64, _ := strconv.ParseUint(account.Uid, 10, 32)
	gid64, _ := strconv.ParseUint(account.Gid, 10, 32)

	root, err := os.MkdirTemp("", "veil-backup-dac-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "lib", "veil")
	statePath := filepath.Join(varDir, "state.json")
	dbPath := filepath.Join(varDir, "veil.db")
	keyPath := filepath.Join(etcDir, "state.key")
	passPath := filepath.Join(etcDir, "backup.passphrase")
	dropInPass := filepath.Join(etcDir, "alt.passphrase")
	backupsDir := filepath.Join(varDir, "backups")
	writeFixture(t, statePath, `{"schemaVersion":1}`)
	writeFixture(t, dbPath, "sqlite")
	writeFixture(t, keyPath, "state-key")
	writeFixture(t, passPath, "scheduled-passphrase-value")
	writeFixture(t, dropInPass, "drop-in-passphrase-value")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(varDir, int(uid64), int(gid64)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(varDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{statePath, dbPath} {
		if err := os.Chown(path, int(uid64), int(gid64)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	script := "set -eu\n" +
		"test -r " + strconv.Quote(statePath) + "\n" +
		"test -r " + strconv.Quote(dbPath) + "\n" +
		"test -r " + strconv.Quote(keyPath) + "\n" +
		"test -r " + strconv.Quote(passPath) + "\n" +
		"test -r " + strconv.Quote(dropInPass) + "\n" +
		"test -w " + strconv.Quote(backupsDir) + "\n"
	run := func(boundingSet string) error {
		args := []string{
			"systemd-run", "--wait", "--pipe", "--collect",
			"--property=User=root",
			"--property=Group=root",
			"--property=NoNewPrivileges=true",
			"--property=ProtectSystem=strict",
			"--property=ProtectHome=yes",
			"--property=PrivateDevices=true",
			"--property=CapabilityBoundingSet=" + boundingSet,
			"--property=RestrictAddressFamilies=AF_UNIX",
			"--property=ReadWritePaths=" + varDir,
			"--property=BindReadOnlyPaths=" + etcDir,
			"/bin/sh", "-c", script,
		}
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return &backupDACError{err: err, output: string(output)}
		}
		return nil
	}

	if err := run(""); err == nil {
		t.Fatal("empty CapabilityBoundingSet unexpectedly read veil-owned state")
	}
	if err := run("CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH"); err != nil {
		t.Fatalf("backup hardening with DAC capabilities could not read veil-owned state: %v", err)
	}
}

type backupDACError struct {
	err    error
	output string
}

func (e *backupDACError) Error() string {
	return strings.TrimSpace(e.err.Error() + "\n" + e.output)
}
