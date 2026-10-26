package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

// pointRotatePreflightAt makes rotate-key scan dir for packaged drop-ins and an
// empty temporary runtime directory, so the host's own /run/systemd/system
// never leaks into a test.
func pointRotatePreflightAt(t *testing.T, dir string) {
	t.Helper()
	original, originalRuntime := rotatePreflightSystemdDir, rotatePreflightRuntimeSystemdDir
	rotatePreflightSystemdDir = dir
	rotatePreflightRuntimeSystemdDir = t.TempDir()
	t.Cleanup(func() {
		rotatePreflightSystemdDir, rotatePreflightRuntimeSystemdDir = original, originalRuntime
	})
}

// pointRotatePreflightRuntimeAt points the runtime drop-in directory at dir.
func pointRotatePreflightRuntimeAt(t *testing.T, dir string) {
	t.Helper()
	original := rotatePreflightRuntimeSystemdDir
	rotatePreflightRuntimeSystemdDir = dir
	t.Cleanup(func() { rotatePreflightRuntimeSystemdDir = original })
}

// writeHelperKeyPinDropIn writes the pre-#1472 helper override that pins the
// key path with Environment= and no EnvironmentFile=.
func writeHelperKeyPinDropIn(t *testing.T, systemdDir, body string) string {
	t.Helper()
	dir := filepath.Join(systemdDir, "veil-helper.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "10-veil-install.conf")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeBackupKeyPinDropIn writes the pre-#1472 backup override that passes the
// key path on the command line.
func writeBackupKeyPinDropIn(t *testing.T, systemdDir, keyPath string) string {
	t.Helper()
	dir := filepath.Join(systemdDir, "veil-backup.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "10-veil-install.conf")
	body := "[Service]\nExecStart=\nExecStart=/usr/local/bin/veil backup create --state /var/lib/veil/state.json --key-path " + keyPath + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertStateDecryptsWith loads the state file with the key stored at keyPath,
// the way the Panel does after a reload.
func assertStateDecryptsWith(t *testing.T, keyPath, statePath string) {
	t.Helper()
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read rotated key: %v", err)
	}
	cipher, err := cipherFromRawKey(raw)
	if err != nil {
		t.Fatalf("rotated key: %v", err)
	}
	if _, ok, err := managementstate.NewStore(statePath, cipher).Load(); err != nil || !ok {
		t.Fatalf("state does not decrypt with the key veil.env names: ok=%v err=%v", ok, err)
	}
}

// TestRotateKeyInPlaceWithAStalePinOnTheLiveKey locks #1483: a pre-#1472
// drop-in that pins exactly the key file veil.env records cannot break an
// in-place rotation, because the rotation rewrites that same file and the unit
// reads it. Refusing here blocked the emergency re-key until `veil repair`.
func TestRotateKeyInPlaceWithAStalePinOnTheLiveKey(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)
	systemdDir := t.TempDir()
	writeHelperKeyPinDropIn(t, systemdDir, "[Service]\nEnvironment=VEIL_KEY_PATH="+keyPath+"\n")
	pointRotatePreflightAt(t, systemdDir)

	before, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("in-place rotation refused by a pin on the live key: %v\n%s", err, out)
	}
	after, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("in-place rotation did not rewrite the key file")
	}
	assertStateDecryptsWith(t, keyPath, statePath)
}

// TestRotateKeyInPlaceWithAStaleBackupPin covers the ExecStart form: the
// backup unit passes --key-path <the live key>, so the in-place rewrite is
// picked up by the next timer run.
func TestRotateKeyInPlaceWithAStaleBackupPin(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)
	systemdDir := t.TempDir()
	writeBackupKeyPinDropIn(t, systemdDir, keyPath)
	pointRotatePreflightAt(t, systemdDir)

	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("in-place rotation refused by a backup pin on the live key: %v\n%s", err, out)
	}
	assertStateDecryptsWith(t, keyPath, statePath)
}

// TestRotateKeyInPlaceStillRefusesAPinOnAnotherKey keeps the invariant #1473
// introduced: a pin that names a different file would leave the helper reading
// the pre-rotation key while the state holds the new one, so the rotation must
// still stop before touching anything.
func TestRotateKeyInPlaceStillRefusesAPinOnAnotherKey(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)
	systemdDir := t.TempDir()
	other := filepath.Join(etcDir, "old-state.key")
	dropIn := writeHelperKeyPinDropIn(t, systemdDir, "[Service]\nEnvironment=VEIL_KEY_PATH="+other+"\n")
	pointRotatePreflightAt(t, systemdDir)

	before, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath)
	if err == nil {
		t.Fatalf("rotate-key ignored a pin on %s:\n%s", other, out)
	}
	for _, want := range []string{dropIn, "veil repair"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
	// The refusal carries the repair command for the installed tree, so the
	// operator does not get a second refusal from repair (#1482 review).
	for _, want := range []string{"--etc-dir " + etcDir, "--var-dir " + filepath.Dir(statePath)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not name %q", err, want)
		}
	}
	if after, err := os.ReadFile(keyPath); err != nil || !bytes.Equal(after, before) {
		t.Fatal("the key changed although the preflight refused")
	}
}

// TestRotateKeyIgnoresACommentedOutKeyPin locks #1484: systemd ignores comments,
// so a commented-out pin sets nothing and must not block a rotation. The
// documented manual fix (comment the literal out) has to work.
func TestRotateKeyIgnoresACommentedOutKeyPin(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)
	systemdDir := t.TempDir()
	writeHelperKeyPinDropIn(t, systemdDir, "[Service]\n# Environment=VEIL_KEY_PATH="+keyPath+"\n")
	pointRotatePreflightAt(t, systemdDir)

	target := filepath.Join(etcDir, "rotated-state.key")
	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", target)
	if err != nil {
		t.Fatalf("rotation blocked by a commented-out pin: %v\n%s", err, out)
	}
	if body, err := os.ReadFile(envPath); err != nil || !strings.Contains(string(body), "VEIL_KEY_PATH="+target) {
		t.Fatalf("veil.env does not name the rotated key:\n%q\n%v", body, err)
	}
	assertStateDecryptsWith(t, target, statePath)
}

// TestStaleKeyPinsScanTheRuntimeUnitDirectory locks the runtime half of the
// #1484 scoping note: drop-ins under /run/systemd/system load after the packaged
// ones and override them, so a pin there is just as live.
func TestStaleKeyPinsScanTheRuntimeUnitDirectory(t *testing.T) {
	packagedDir := t.TempDir()
	runtimeDir := t.TempDir()
	path := writeHelperKeyPinDropIn(t, runtimeDir, "[Service]\nEnvironment=VEIL_KEY_PATH=/opt/veil/etc/state.key\n")

	pins := staleKeyPins(packagedDir, runtimeDir)
	if len(pins) != 1 || pins[0].DropIn != path {
		t.Fatalf("staleKeyPins = %+v, want the runtime drop-in %s", pins, path)
	}
	// The same directory passed twice must not report the pin twice.
	if again := staleKeyPins(runtimeDir, runtimeDir); len(again) != 1 {
		t.Fatalf("duplicate directory reported %d pins", len(again))
	}
}

// TestRotateKeySeesATransientDropIn proves the command wires the runtime
// directory through: a pin written there blocks a rotation that moves the key.
func TestRotateKeySeesATransientDropIn(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)
	pointRotatePreflightAt(t, t.TempDir())
	runtimeDir := t.TempDir()
	pointRotatePreflightRuntimeAt(t, runtimeDir)
	dropIn := writeHelperKeyPinDropIn(t, runtimeDir, "[Service]\nEnvironment=VEIL_KEY_PATH=/opt/veil/etc/state.key\n")

	before, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", filepath.Join(etcDir, "rotated-state.key"))
	if err == nil {
		t.Fatalf("rotate-key ignored a transient runtime drop-in:\n%s", out)
	}
	if !strings.Contains(err.Error(), dropIn) {
		t.Fatalf("error %v does not name %q", err, dropIn)
	}
	if after, err := os.ReadFile(keyPath); err != nil || !bytes.Equal(after, before) {
		t.Fatal("the key changed although the preflight refused")
	}
}
