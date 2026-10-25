package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// stubAdminNotifyPanel keeps the rotation tests off the host's systemd.
func stubAdminNotifyPanel(t *testing.T) {
	t.Helper()
	original := adminNotifyPanel
	adminNotifyPanel = func() error { return nil }
	t.Cleanup(func() { adminNotifyPanel = original })
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
	stubAdminNotifyPanel(t)
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)

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
	stubAdminNotifyPanel(t)
	etcDir, keyPath, statePath, _ := seedRotationFixture(t)
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
}

// TestAdminRotateKeyInPlaceLeavesTheEnvironmentAlone keeps the in-place path
// free of surprises: with no --new-key-path the environment file is not
// rewritten at all.
func TestAdminRotateKeyInPlaceLeavesTheEnvironmentAlone(t *testing.T) {
	stubAdminNotifyPanel(t)
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)

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
}
