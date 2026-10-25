package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// restoreEnvFixture builds an installed-looking layout: veil.env naming the
// live key and state path, a state file encrypted with that key, and the SQLite
// store every current archive carries.
func restoreEnvFixture(t *testing.T) (etcDir, envPath, keyPath, statePath string) {
	t.Helper()
	etcDir = t.TempDir()
	varDir := t.TempDir()
	keyPath = filepath.Join(etcDir, "state.key")
	statePath = filepath.Join(varDir, "state.json")
	envPath = filepath.Join(etcDir, "veil.env")
	env := "VEIL_KEY_PATH=" + keyPath + "\nVEIL_STATE_PATH=" + statePath + "\n"
	if err := os.WriteFile(envPath, []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.ManagementSnapshot{Users: []model.User{{
		Username: "admin", PasswordHash: "hash", Role: "admin", TOTPSecret: rotationTOTPSecret,
	}}}
	if err := managementstate.EncryptSnapshot(&snapshot, cipher); err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatal(err)
	}
	createCLIBackupDatabase(t, statePath)
	return etcDir, envPath, keyPath, statePath
}

func createArchive(t *testing.T, statePath, keyPath, archive string) {
	t.Helper()
	cmd := NewRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", archive,
		"--allow-unencrypted",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("backup create: %v", err)
	}
}

// TestCLIBackupRestoreUsesInstalledVeilEnvKeyPath locks #1474: the restore
// resolves state/key from the installed veil.env, so a pre-rotation archive is
// written to the path the Panel reads. Before this, the packaged default was
// used, the rotated key stayed where the Panel looked, and the restored state
// failed to decrypt with a GCM error after a "successful" restore.
func TestCLIBackupRestoreUsesInstalledVeilEnvKeyPath(t *testing.T) {
	etcDir, envPath, keyPath, statePath := restoreEnvFixture(t)
	archive := filepath.Join(t.TempDir(), "pre-rotation.tar.gz")
	createArchive(t, statePath, keyPath, archive)

	// Rotate to another key path, exactly as the documented procedure does.
	systemctlCalls, _ := stubAdminSystemctl(t, envPath)
	rotatedKeyPath := filepath.Join(etcDir, "rotated-state.key")
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", rotatedKeyPath); err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}
	if len(*systemctlCalls) == 0 {
		t.Fatal("rotation did not touch systemd")
	}
	// The key live at the moment of the restore; it must stop decrypting the
	// restored (pre-rotation) state afterwards.
	preRestoreKey, err := os.ReadFile(rotatedKeyPath)
	if err != nil {
		t.Fatal(err)
	}

	// Restore the pre-rotation archive without pointing at any path: the CLI
	// has to use the installed environment.
	restore := NewRootCommand("test")
	out := &bytes.Buffer{}
	restore.SetOut(out)
	restore.SetErr(out)
	restore.SetArgs([]string{"backup", "restore", archive, "--allow-unencrypted", "--yes"})
	if err := restore.Execute(); err != nil {
		t.Fatalf("backup restore: %v\n%s", err, out.String())
	}

	envBody, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	installedKeyPath := veilenv.Value(envBody, veilenv.KeyPathKey)
	if installedKeyPath != rotatedKeyPath {
		t.Fatalf("restore moved the environment key path: %q, want %q", installedKeyPath, rotatedKeyPath)
	}
	// The Panel loads the key from the environment path; the restored state has
	// to decrypt with it.
	keyBytes, err := os.ReadFile(installedKeyPath)
	if err != nil {
		t.Fatalf("read the key the Panel loads: %v", err)
	}
	cipher, err := cipherFromRawKey(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("restored state does not load with the environment key: ok=%t err=%v", ok, err)
	}
	if len(snapshot.Users) != 1 || snapshot.Users[0].Username != "admin" {
		t.Fatalf("restored users = %+v", snapshot.Users)
	}
	// The pre-restore (rotated) key no longer decrypts the restored state,
	// which proves the restore actually replaced key and state together.
	preRestoreCipher, err := cipherFromRawKey(preRestoreKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := managementstate.NewStore(statePath, preRestoreCipher).Load(); err == nil {
		t.Fatal("the pre-restore key still decrypts the restored state")
	}
}

// TestCLIBackupRestoreExplicitKeyPathRepointsEnvironment locks the second half
// of #1474: when the operator does point the restore at another key file, the
// installed VEIL_KEY_PATH is repointed to it, so state and the key the Panel
// reads never diverge.
func TestCLIBackupRestoreExplicitKeyPathRepointsEnvironment(t *testing.T) {
	etcDir, envPath, keyPath, statePath := restoreEnvFixture(t)
	archive := filepath.Join(t.TempDir(), "restore.tar.gz")
	createArchive(t, statePath, keyPath, archive)
	movedKeyPath := filepath.Join(etcDir, "moved-state.key")

	// The Panel is stopped, so the archive may carry veil.db (#1452).
	systemctlCalls, _ := stubAdminSystemctl(t, envPath)
	restore := NewRootCommand("test")
	out := &bytes.Buffer{}
	restore.SetOut(out)
	restore.SetErr(out)
	restore.SetArgs([]string{
		"backup", "restore", archive,
		"--state", statePath,
		"--key-path", movedKeyPath,
		"--allow-unencrypted", "--yes",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("backup restore: %v\n%s", err, out.String())
	}
	envBody, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := veilenv.Value(envBody, veilenv.KeyPathKey); got != movedKeyPath {
		t.Fatalf("VEIL_KEY_PATH = %q, want %q", got, movedKeyPath)
	}
	if !strings.Contains(out.String(), "now points at the restored key") {
		t.Fatalf("restore did not report the environment change:\n%s", out.String())
	}
	keyBytes, err := os.ReadFile(movedKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cipherFromRawKey(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := managementstate.NewStore(statePath, cipher).Load(); err != nil || !ok {
		t.Fatalf("restored state does not load with the repointed key: ok=%t err=%v", ok, err)
	}
	for _, call := range *systemctlCalls {
		if strings.HasPrefix(call, "kill ") {
			t.Fatalf("a path-changing restore must restart, not SIGHUP: %v", *systemctlCalls)
		}
	}
}
