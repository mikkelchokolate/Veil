package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// stubAdminSystemctl keeps the rotation tests off the host's systemd and
// records what the command asked it to do. When a restart is recorded it also
// snapshots VEIL_KEY_PATH, so a test can prove the environment was already
// repointed before the Panel was told to restart (#1453 review).
func stubAdminSystemctl(t *testing.T, envPath string) (*[]string, *map[string]string) {
	t.Helper()
	original := adminSystemctlRun
	var calls []string
	envAtRestart := map[string]string{}
	adminSystemctlRun = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 1 && args[0] == "restart" {
			if body, err := os.ReadFile(envPath); err == nil {
				envAtRestart[args[1]] = veilenv.Value(body, veilenv.KeyPathKey)
			}
		}
		return nil
	}
	t.Cleanup(func() { adminSystemctlRun = original })
	return &calls, &envAtRestart
}

// cipherFromRawKey turns key material read back from disk into a cipher, the
// way the Panel does when it loads VEIL_KEY_PATH.
// rotationTOTPSecret is the second-factor seed the rotation fixture seals, so
// the state file is genuinely encrypted under the live key.
const rotationTOTPSecret = "JBSWY3DPEHPK3PXP"

func cipherFromRawKey(raw []byte) (*secrets.Cipher, error) {
	if len(raw) != secrets.KeySize {
		return nil, fmt.Errorf("key has %d bytes, want %d", len(raw), secrets.KeySize)
	}
	var key [secrets.KeySize]byte
	copy(key[:], raw)
	return secrets.NewCipher(key)
}

// seedRotationFixture writes a Panel environment file, an encryption key and a
// state file with one user, the way an installed host looks.
func seedRotationFixture(t *testing.T) (etcDir, keyPath, statePath, envPath string) {
	t.Helper()
	etcDir = t.TempDir()
	keyPath = filepath.Join(etcDir, "state.key")
	statePath = filepath.Join(t.TempDir(), "state.json")
	envPath = filepath.Join(etcDir, "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_KEY_PATH="+keyPath+"\nVEIL_LISTEN=127.0.0.1:2096\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatal(err)
	}
	// A second factor secret makes the snapshot actually encrypted: with only
	// plaintext fields the state file decrypts under any key.
	snapshot := model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin", TOTPSecret: rotationTOTPSecret}},
	}
	if err := managementstate.EncryptSnapshot(&snapshot, cipher); err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatal(err)
	}
	// Key rotation re-encrypts the SQLite credential store next to state.
	createAdminRotateDatabase(t, filepath.Dir(statePath))
	return etcDir, keyPath, statePath, envPath
}

func runRotateKey(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newFastRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"admin", "rotate-key"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

// TestAdminRotateKeyRepointsPanelEnvBeforeReload locks #1453: rotating to
// another path must repoint VEIL_KEY_PATH in the installed environment file,
// because the Panel, the privileged helper and the backup unit read the key
// from there. Otherwise the command re-encrypts state under a key nothing
// loads, reloads the Panel into a broken key and the documented procedure
// causes a credential-decryption outage.
func TestAdminRotateKeyRepointsPanelEnvBeforeReload(t *testing.T) {

	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	// The Panel resolves VEIL_KEY_PATH at startup, so a rotation that moves the
	// key must restart the unit; SIGHUP keeps the old environment.
	systemctlCalls, envAtRestart := stubAdminSystemctl(t, envPath)

	oldKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newKeyPath := filepath.Join(etcDir, "rotated-state.key")

	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", newKeyPath)
	if err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}

	// The units' environment now names the new key, with every other line and
	// the file mode intact.
	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := veilenv.Value(body, veilenv.KeyPathKey); got != newKeyPath {
		t.Fatalf("VEIL_KEY_PATH = %q, want %q\n%s", got, newKeyPath, body)
	}
	if !strings.Contains(string(body), "VEIL_LISTEN=127.0.0.1:2096\n") {
		t.Fatalf("unrelated environment line lost:\n%s", body)
	}
	if info, err := os.Stat(envPath); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("environment mode = %v err=%v, want 0640", info, err)
	}

	// The Panel was restarted, not signalled, and by then the environment
	// already named the new key — otherwise it would reload the old one.
	for _, unit := range []string{"veil.service", "veil-helper.service"} {
		if !strings.Contains("\n"+strings.Join(*systemctlCalls, "\n")+"\n", "\nrestart "+unit+"\n") {
			t.Fatalf("rotate-key must restart %s to re-read the environment; calls=%v", unit, *systemctlCalls)
		}
		if got := (*envAtRestart)[unit]; got != newKeyPath {
			t.Fatalf("%s restarted while VEIL_KEY_PATH was %q, want %q", unit, got, newKeyPath)
		}
	}
	if strings.Contains("\n"+strings.Join(*systemctlCalls, "\n")+"\n", "\nkill -s HUP veil.service\n") {
		t.Fatalf("rotate-key must not SIGHUP the Panel, which keeps the old environment; calls=%v", *systemctlCalls)
	}

	// What the Panel loads from the environment decrypts the rotated state …
	loadedKey, err := os.ReadFile(veilenv.Value(body, veilenv.KeyPathKey))
	if err != nil {
		t.Fatalf("read the key the environment points at: %v", err)
	}
	loadedCipher, err := cipherFromRawKey(loadedKey)
	if err != nil {
		t.Fatalf("cipher from the environment key: %v", err)
	}
	snapshot, ok, err := managementstate.NewStore(statePath, loadedCipher).Load()
	if err != nil || !ok {
		t.Fatalf("state does not load with the key VEIL_KEY_PATH names: ok=%t err=%v", ok, err)
	}
	if len(snapshot.Users) != 1 || snapshot.Users[0].Username != "admin" {
		t.Fatalf("rotated state = %+v", snapshot.Users)
	}
	if snapshot.Users[0].TOTPSecret != rotationTOTPSecret {
		t.Fatalf("second factor after rotation = %q, want the seeded value", snapshot.Users[0].TOTPSecret)
	}

	// … and the old key no longer does, so this is a real rotation.
	oldCipher, err := cipherFromRawKey(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := managementstate.NewStore(statePath, oldCipher).Load(); err == nil {
		t.Fatal("the old key still decrypts the rotated state")
	}
}

// TestAdminRotateKeyRefusesBeforeRotatingWhenTheEnvCannotBeRepointed locks the
// other half of #1453: the command must never leave state re-encrypted under a
// key the Panel does not load. When the environment file cannot be rewritten
// (here: it does not exist), it fails before touching state or writing a key.
func TestAdminRotateKeyRefusesBeforeRotatingWhenTheEnvCannotBeRepointed(t *testing.T) {

	etcDir, keyPath, statePath, _ := seedRotationFixture(t)
	systemctlCalls, _ := stubAdminSystemctl(t, "")
	// Drop the environment file: nothing can be repointed.
	if err := os.Remove(filepath.Join(etcDir, "veil.env")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)

	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newKeyPath := filepath.Join(etcDir, "rotated-state.key")

	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", newKeyPath)
	if err == nil {
		t.Fatalf("rotate-key succeeded without a repointable environment:\n%s", out)
	}
	if !strings.Contains(err.Error(), "refusing to rotate the state key") {
		t.Fatalf("error = %v, want a refusal before rotation", err)
	}
	if _, statErr := os.Stat(newKeyPath); !os.IsNotExist(statErr) {
		t.Fatalf("a new key file was written although the command refused: %v", statErr)
	}
	if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(after, stateBefore) {
		t.Fatalf("state changed although the command refused: err=%v equal=%t", err, bytes.Equal(after, stateBefore))
	}
	if after, err := os.ReadFile(keyPath); err != nil || !bytes.Equal(after, keyBefore) {
		t.Fatalf("live key changed although the command refused: err=%v equal=%t", err, bytes.Equal(after, keyBefore))
	}
	if len(*systemctlCalls) != 0 {
		t.Fatalf("the Panel was signalled although the command refused before rotating: %v", *systemctlCalls)
	}
}

// TestAdminRotateKeyInPlaceLeavesTheEnvironmentAlone keeps the in-place path
// free of surprises: with no --new-key-path the environment file is not
// rewritten at all.
func TestAdminRotateKeyInPlaceLeavesTheEnvironmentAlone(t *testing.T) {

	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	systemctlCalls, _ := stubAdminSystemctl(t, envPath)

	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}
	after, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("in-place rotation rewrote the environment:\nbefore %q\nafter  %q", before, after)
	}
	// In place, the environment did not change, so the Panel reloads it with
	// SIGHUP as before instead of being restarted.
	if !strings.Contains("\n"+strings.Join(*systemctlCalls, "\n")+"\n", "\nkill -s HUP veil.service\n") {
		t.Fatalf("in-place rotation must reload the Panel with SIGHUP; calls=%v", *systemctlCalls)
	}
	if strings.Contains(strings.Join(*systemctlCalls, "\n"), "restart veil.service") {
		t.Fatalf("in-place rotation must not restart the Panel; calls=%v", *systemctlCalls)
	}
}

// TestRotatedKeyPathProducesAVerifiableBackup locks the second half of #1471:
// the backup timer takes its key path from the same environment file, so after
// a rotation it archives post-rotation state with the post-rotation key and the
// archive verifies. With the pre-rotation key the same archive would be
// rejected, which is what silently stopped scheduled backups.
func TestRotatedKeyPathProducesAVerifiableBackup(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)
	oldKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newKeyPath := filepath.Join(etcDir, "rotated-state.key")
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", newKeyPath); err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}
	envBody, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	keyPathFromEnv := veilenv.Value(envBody, veilenv.KeyPathKey)
	if keyPathFromEnv != newKeyPath {
		t.Fatalf("environment key path = %q, want %q", keyPathFromEnv, newKeyPath)
	}

	const passphrase = "0123456789abcdef"
	archive, err := backup.CreateBackup(statePath, keyPathFromEnv, passphrase)
	if err != nil {
		t.Fatalf("the timer's backup path with the environment key failed: %v", err)
	}
	if _, err := backup.VerifyBackup(archive, passphrase); err != nil {
		t.Fatalf("archive created after the rotation does not verify: %v", err)
	}

	// The old key is what used to be hardcoded into the unit; it no longer
	// matches the state and must be rejected, which is the regression the fix
	// removes.
	oldKeyFile, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if oldKeyFile.Size() != int64(len(oldKey)) {
		t.Fatalf("live key file changed size: %d", oldKeyFile.Size())
	}
	if _, err := backup.CreateBackup(statePath, keyPath, passphrase); err == nil {
		// CreateBackup tolerates some mismatches; verify is the contract the
		// timer relies on.
		mismatched, createErr := backup.CreateBackup(statePath, keyPath, passphrase)
		if createErr == nil {
			if _, verifyErr := backup.VerifyBackup(mismatched, passphrase); verifyErr == nil {
				t.Fatal("an archive built from inconsistent key/state verified")
			}
		}
	}
}

// TestRotateKeyRestartsThePanelBeforeTheHelper locks the ordering review point:
// the Panel writes state, so it must be the first process on the new key. If
// the helper restart is the one that fails, the Panel is already consistent and
// the command reports the failure instead of reporting success with a Panel
// still holding the pre-rotation cipher.
func TestRotateKeyRestartsThePanelBeforeTheHelper(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	calls, _ := stubAdminSystemctl(t, envPath)
	newKeyPath := filepath.Join(etcDir, "rotated-state.key")
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", newKeyPath); err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}
	panel, helper := -1, -1
	for i, call := range *calls {
		switch call {
		case "restart veil.service":
			panel = i
		case "restart veil-helper.service":
			helper = i
		}
	}
	if panel < 0 || helper < 0 {
		t.Fatalf("both units must be restarted; calls=%v", *calls)
	}
	if panel > helper {
		t.Fatalf("the Panel must be restarted before the helper; calls=%v", *calls)
	}
}
