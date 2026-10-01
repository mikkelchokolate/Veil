package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backupsftp"
	"github.com/mikkelchokolate/Veil/internal/testutil/sftpfake"
)

func stubCLISftpDial(t *testing.T, remote *sftpfake.MemFS, dialErr error) {
	t.Helper()
	original := backupsftp.Dial
	backupsftp.Dial = func(context.Context, backupsftp.Config, string) (backupsftp.RemoteFS, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return remote, nil
	}
	t.Cleanup(func() { backupsftp.Dial = original })
}

func writeCLISftpConfig(t *testing.T, enabled bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), backupsftp.ConfigFileName)
	err := backupsftp.SaveConfig(path, backupsftp.Config{
		Enabled: enabled, Host: "backups.example.com", User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: backupsftp.AuthTypePassword, Password: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// cliSftpInstallID is the fixed per-test remote namespace identity; the CLI
// engine persists it in the state dir it derives from --state/--dir (#1184).
const cliSftpInstallID = "0123456789abcdef0123456789abcdef"

// cliSftpNamespace is this node's remote subdirectory under the shared
// remoteDir.
const cliSftpNamespace = "/srv/veil-backups/veil-node-" + cliSftpInstallID

// writeCLISftpInstallID persists the identity file in the dir the engine
// resolves as its state dir, so remote paths are deterministic.
func writeCLISftpInstallID(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(stateDir, backupsftp.InstallIDFileName), []byte(cliSftpInstallID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCreateUploadsToConfiguredSftpCLI(t *testing.T) {
	remote := sftpfake.New()
	stubCLISftpDial(t, remote, nil)
	statePath, keyPath := writeCLIBackupSource(t)
	writeCLISftpInstallID(t, filepath.Dir(statePath))
	outputDir := t.TempDir()
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("scheduled-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeCLISftpConfig(t, true)

	command := NewRootCommand("0.6.0")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--output-dir", outputDir,
		"--sftp-config", configPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("create: %v output=%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Uploaded to SFTP destination") {
		t.Fatalf("output=%s", output.String())
	}
	uploaded := false
	for _, p := range remote.Paths() {
		if strings.HasPrefix(p, cliSftpNamespace+"/veil_backup_") && strings.HasSuffix(p, ".tar.gz.enc") {
			uploaded = true
		}
	}
	if !uploaded {
		t.Fatalf("remote paths=%v", remote.Paths())
	}
	// Status file lands beside the state file — the state dir the scheduled
	// unit mounts writable.
	status := backupsftp.LoadStatus(filepath.Join(filepath.Dir(statePath), backupsftp.StatusFileName))
	if status.LastUploadAt == "" {
		t.Fatal("status not recorded")
	}
}

func TestBackupCreateSftpFailureWarnsButSucceedsCLI(t *testing.T) {
	stubCLISftpDial(t, sftpfake.New(), errors.New("connection refused"))
	statePath, keyPath := writeCLIBackupSource(t)
	outputDir := t.TempDir()
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("scheduled-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeCLISftpConfig(t, true)

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
		"--sftp-config", configPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("create must succeed despite remote failure: %v", err)
	}
	if !strings.Contains(stderr.String(), "SFTP remote upload failed") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	locals, err := filepath.Glob(filepath.Join(outputDir, "veil_backup_*.tar.gz.enc"))
	if err != nil || len(locals) != 1 {
		t.Fatalf("local archives=%v err=%v", locals, err)
	}
}

func TestBackupCreateSftpDisabledSkipsUploadCLI(t *testing.T) {
	remote := sftpfake.New()
	stubCLISftpDial(t, remote, nil)
	statePath, keyPath := writeCLIBackupSource(t)
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("scheduled-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeCLISftpConfig(t, false)

	command := NewRootCommand("0.6.0")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--output-dir", t.TempDir(),
		"--sftp-config", configPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("create: %v output=%s", err, output.String())
	}
	if len(remote.Paths()) != 0 || strings.Contains(output.String(), "SFTP") {
		t.Fatalf("disabled destination touched remote: %v %s", remote.Paths(), output.String())
	}
}

func TestBackupPruneMirrorsRemoteRetentionCLI(t *testing.T) {
	remote := sftpfake.New()
	stubCLISftpDial(t, remote, nil)
	// #1209: the SFTP state dir resolves from the state file's directory —
	// the same derivation `backup create` uses — not from --dir's parent, so
	// an operator-chosen backup dir never mints a second remote namespace.
	stateDir := t.TempDir()
	t.Setenv("VEIL_STATE_PATH", filepath.Join(stateDir, "state.json"))
	// --dir deliberately sits in an unrelated tree: remote state must not
	// be derived from it.
	backupRoot := t.TempDir()
	backupDir := filepath.Join(backupRoot, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCLISftpInstallID(t, stateDir)
	configPath := writeCLISftpConfig(t, true)
	for _, name := range []string{
		"veil_backup_20260201_020000.tar.gz.enc",
		"veil_backup_20260202_020000.tar.gz.enc",
		"veil_backup_20260203_020000.tar.gz.enc",
	} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("local"), 0o600); err != nil {
			t.Fatal(err)
		}
		remote.SetFile(path.Join(cliSftpNamespace, name), []byte("remote"))
	}
	// A foreign archive at the shared remoteDir root is not a prune
	// candidate (#1184).
	remote.SetFile("/srv/veil-backups/veil_backup_20260204_020000.tar.gz.enc", []byte("foreign"))

	command := NewRootCommand("0.6.0")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"backup", "prune",
		"--dir", backupDir,
		"--daily", "1", "--weekly", "0", "--monthly", "0",
		"--sftp-config", configPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("prune: %v output=%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Remote retention") {
		t.Fatalf("output=%s", output.String())
	}
	kept := 0
	for _, p := range remote.Paths() {
		if strings.HasPrefix(p, cliSftpNamespace+"/") && strings.HasSuffix(p, ".tar.gz.enc") {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("remote paths=%v", remote.Paths())
	}
	if !remote.Has("/srv/veil-backups/veil_backup_20260204_020000.tar.gz.enc") {
		t.Fatal("foreign archive outside the node namespace was pruned")
	}
	// The remote namespace identity must come from the state dir, never
	// minted fresh beside --dir.
	if _, err := os.Stat(filepath.Join(backupRoot, backupsftp.InstallIDFileName)); !os.IsNotExist(err) {
		t.Fatal("install id minted beside --dir instead of the state dir")
	}
}

// #1188: --allow-unencrypted consents to a local plaintext archive only —
// the configured SFTP destination must never receive it.
func TestBackupCreateAllowUnencryptedSkipsSftpUploadCLI(t *testing.T) {
	remote := sftpfake.New()
	stubCLISftpDial(t, remote, nil)
	statePath, keyPath := writeCLIBackupSource(t)
	writeCLISftpInstallID(t, filepath.Dir(statePath))
	outputDir := t.TempDir()
	configPath := writeCLISftpConfig(t, true)

	command := NewRootCommand("0.6.0")
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--allow-unencrypted",
		"--output-dir", outputDir,
		"--sftp-config", configPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("create: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "plaintext archive not sent to the SFTP destination") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	locals, err := filepath.Glob(filepath.Join(outputDir, "veil_backup_*.tar.gz"))
	if err != nil || len(locals) != 1 {
		t.Fatalf("local plaintext archive missing: %v err=%v", locals, err)
	}
	if len(remote.Paths()) != 0 || len(remote.Written) != 0 {
		t.Fatalf("plaintext archive reached the remote: %v", remote.Paths())
	}
}

func TestBackupPruneDryRunSkipsRemoteCLI(t *testing.T) {
	remote := sftpfake.New()
	stubCLISftpDial(t, remote, nil)
	backupDir := t.TempDir()
	configPath := writeCLISftpConfig(t, true)
	if err := os.WriteFile(filepath.Join(backupDir, "veil_backup_20260201_020000.tar.gz.enc"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	remote.SetFile("/srv/veil-backups/veil_backup_20260201_020000.tar.gz.enc", []byte("remote"))

	command := NewRootCommand("0.6.0")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{
		"backup", "prune",
		"--dir", backupDir,
		"--daily", "0", "--weekly", "0", "--monthly", "0",
		"--dry-run",
		"--sftp-config", configPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("prune: %v output=%s", err, output.String())
	}
	if len(remote.Paths()) != 1 || strings.Contains(output.String(), "Remote retention") {
		t.Fatalf("dry-run touched remote: %v %s", remote.Paths(), output.String())
	}
}
