package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStaleHelperDropIn reproduces a pre-#1472 custom-install unit override:
// the key path is pinned with Environment= and there is no EnvironmentFile=.
func writeStaleHelperDropIn(t *testing.T, systemdDir string) string {
	t.Helper()
	dir := filepath.Join(systemdDir, "veil-helper.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "10-veil-install.conf")
	body := "[Service]\nEnvironment=VEIL_KEY_PATH=/opt/veil/etc/state.key\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStaleKeyPinDropInsDetectsBothPre1472Forms locks the scanner: the helper
// form (Environment= literal, no EnvironmentFile=) and the backup form
// (--key-path in ExecStart). A refreshed drop-in that reads veil.env is clean.
// The pinned path is part of the result, because rotate-key has to tell a pin
// on the rotated key from a pin on another file (#1483).
func TestStaleKeyPinDropInsDetectsBothPre1472Forms(t *testing.T) {
	systemdDir := t.TempDir()
	helper := writeStaleHelperDropIn(t, systemdDir)
	backupDir := filepath.Join(systemdDir, "veil-backup.service.d")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupDir, "10-veil-install.conf")
	if err := os.WriteFile(backup, []byte("[Service]\nExecStart=\nExecStart=/usr/local/bin/veil backup create --state /opt/veil/var/state.json --key-path /opt/veil/etc/state.key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanDir := filepath.Join(systemdDir, "veil.service.d")
	if err := os.MkdirAll(cleanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cleanDir, "10-veil-install.conf"), []byte("[Service]\nEnvironmentFile=-\nEnvironmentFile=-/opt/veil/etc/veil.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := staleKeyPins(systemdDir)
	want := []keyPin{
		{DropIn: backup, Pinned: "/opt/veil/etc/state.key"},
		{DropIn: helper, Pinned: "/opt/veil/etc/state.key"},
	}
	if len(got) != len(want) {
		t.Fatalf("staleKeyPins = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("staleKeyPins[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestStaleKeyPinsUnderstandsBothExecStartSpellings covers the two forms a
// backup drop-in can use for the flag; a value this parser misses would look
// unparsable and keep the in-place rotation refused.
func TestStaleKeyPinsUnderstandsBothExecStartSpellings(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{"separate argument", "ExecStart=/usr/local/bin/veil backup create --state /s --key-path /opt/veil/etc/state.key", "/opt/veil/etc/state.key"},
		{"equals form", "ExecStart=/usr/local/bin/veil backup create --state /s --key-path=/opt/veil/etc/state.key", "/opt/veil/etc/state.key"},
		{"quoted value", "ExecStart=/usr/local/bin/veil backup create --key-path \"/opt/veil/etc/state.key\"", "/opt/veil/etc/state.key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			systemdDir := t.TempDir()
			dir := filepath.Join(systemdDir, "veil-backup.service.d")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := "[Service]\n" + tc.line + "\n"
			if err := os.WriteFile(filepath.Join(dir, "10-veil-install.conf"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			pins := staleKeyPins(systemdDir)
			if len(pins) != 1 || pins[0].Pinned != tc.want {
				t.Fatalf("staleKeyPins = %+v, want pinned %q", pins, tc.want)
			}
		})
	}
}

// TestRotateKeyRefusesStalePre1472DropIns locks the preflight: a rotation would
// repoint veil.env while the helper/backup drop-ins keep the literal, leaving
// the timer on the pre-rotation key. The command refuses before touching
// anything and points at `veil repair`.
func TestRotateKeyRefusesStalePre1472DropIns(t *testing.T) {
	etcDir, envPath, keyPath, statePath := restoreEnvFixture(t)
	stubAdminSystemctl(t, envPath)
	systemdDir := t.TempDir()
	stale := writeStaleHelperDropIn(t, systemdDir)
	original := rotatePreflightSystemdDir
	rotatePreflightSystemdDir = systemdDir
	t.Cleanup(func() { rotatePreflightSystemdDir = original })

	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", filepath.Join(etcDir, "rotated.key"))
	if err == nil {
		t.Fatalf("rotate-key ignored a stale pre-#1472 drop-in:\n%s", out)
	}
	for _, want := range []string{stale, "veil repair"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
	if after, err := os.ReadFile(keyPath); err != nil || !bytes.Equal(after, keyBefore) {
		t.Fatal("the key changed although the preflight refused")
	}

	// Re-rendered drop-ins (what `veil repair` writes) let the rotation run.
	if err := os.Remove(stale); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("[Service]\nEnvironmentFile=-\nEnvironmentFile=-"+envPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", filepath.Join(etcDir, "rotated.key")); err != nil {
		t.Fatalf("rotation after the repair-shaped drop-in: %v\n%s", err, out)
	}
}
