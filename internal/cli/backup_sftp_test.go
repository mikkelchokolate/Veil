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

func TestBackupCreateUploadsToConfiguredSftpCLI(t *testing.T) {
	remote := sftpfake.New()
	stubCLISftpDial(t, remote, nil)
	statePath, keyPath := writeCLIBackupSource(t)
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
		if strings.HasPrefix(p, "/srv/veil-backups/veil_backup_") && strings.HasSuffix(p, ".tar.gz.enc") {
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
	backupDir := t.TempDir()
	configPath := writeCLISftpConfig(t, true)
	for _, name := range []string{
		"veil_backup_20260201_020000.tar.gz.enc",
		"veil_backup_20260202_020000.tar.gz.enc",
		"veil_backup_20260203_020000.tar.gz.enc",
	} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("local"), 0o600); err != nil {
			t.Fatal(err)
		}
		remote.SetFile(path.Join("/srv/veil-backups", name), []byte("remote"))
	}

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
		if strings.HasSuffix(p, ".tar.gz.enc") {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("remote paths=%v", remote.Paths())
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
