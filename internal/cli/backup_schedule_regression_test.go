package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

func TestBackupScheduleEnableWiresCustomPassphrasePathIntoSystemdService(t *testing.T) {
	for _, tc := range []struct {
		name            string
		existingDefault bool
	}{
		{name: "absentDefaultPassphraseFile", existingDefault: false},
		{name: "existingDefaultPassphraseFile", existingDefault: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := t.TempDir()
			systemdDir := filepath.Join(host, "systemd")
			defaultPassPath := filepath.Join(host, "etc", "veil", "backup.passphrase")
			customPassPath := filepath.Join(host, "etc", "veil", "custom.passphrase")
			if err := os.MkdirAll(filepath.Dir(defaultPassPath), 0o700); err != nil {
				t.Fatal(err)
			}
			unitBody := writeBackupServiceFixture(t, systemdDir, defaultPassPath)

			defaultPassphrase := "default-path-passphrase"
			customPassphrase := "custom-path-passphrase"
			if tc.existingDefault {
				if err := os.WriteFile(defaultPassPath, []byte(defaultPassphrase+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var reloadSawDropIn bool
			restore := stubBackupScheduleSystemd(t, systemdDir, func(args []string) error {
				if len(args) > 0 && args[0] == "daemon-reload" {
					if _, err := os.Stat(backupScheduleDropInPath(systemdDir)); err != nil {
						t.Errorf("drop-in missing at daemon-reload: %v", err)
					} else {
						reloadSawDropIn = true
					}
				}
				return nil
			})
			defer restore()

			cmd := NewRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{
				"backup", "schedule", "enable",
				"--passphrase", customPassphrase,
				"--passphrase-path", customPassPath,
			})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("schedule enable: %v\n%s", err, out.String())
			}

			dropInBody, err := os.ReadFile(backupScheduleDropInPath(systemdDir))
			if err != nil {
				t.Fatalf("expected systemd drop-in for custom passphrase path: %v", err)
			}
			if !reloadSawDropIn {
				t.Fatal("daemon-reload ran before the custom passphrase drop-in was published")
			}

			effectivePassPath := passphraseFileFromBackupService(t, unitBody, string(dropInBody))
			if !sameBackupPath(effectivePassPath, customPassPath) {
				t.Fatalf("effective service passphrase-file = %q, want custom %q\ndrop-in:\n%s", effectivePassPath, customPassPath, dropInBody)
			}
			if sameBackupPath(effectivePassPath, defaultPassPath) {
				t.Fatalf("effective service still points at the default passphrase path:\n%s", dropInBody)
			}
			if condition := effectiveSystemdDirective([]string{unitBody, string(dropInBody)}, "ConditionPathExists"); !sameBackupPath(condition, customPassPath) {
				t.Fatalf("ConditionPathExists = %q, want custom %q\ndrop-in:\n%s", condition, customPassPath, dropInBody)
			}

			stored, err := os.ReadFile(customPassPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(stored) != customPassphrase+"\n" {
				t.Fatalf("custom passphrase file = %q", stored)
			}
			if tc.existingDefault {
				got, err := os.ReadFile(defaultPassPath)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != defaultPassphrase+"\n" {
					t.Fatalf("default passphrase file mutated: %q", got)
				}
			} else if _, err := os.Stat(defaultPassPath); !os.IsNotExist(err) {
				t.Fatalf("default passphrase file should stay absent, err=%v", err)
			}

			statePath, keyPath := writeCLIBackupSource(t)
			outputDir := t.TempDir()
			archive := runBackupCreateOneShot(t, effectivePassPath, statePath, keyPath, outputDir)
			if _, err := backup.VerifyBackupFile(archive, customPassphrase, 0); err != nil {
				t.Fatalf("verify with custom passphrase: %v", err)
			}
			if tc.existingDefault {
				if _, err := backup.VerifyBackupFile(archive, defaultPassphrase, 0); err == nil {
					t.Fatal("archive verified with the default-path passphrase; service used the wrong key")
				}
			}
		})
	}
}

func TestBackupScheduleEnableRejectsProtectHomeHiddenPassphrasePath(t *testing.T) {
	restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), nil)
	defer restore()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"backup", "schedule", "enable",
		"--passphrase", "custom-path-passphrase",
		"--passphrase-path", "/root/veil-backup.passphrase",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "ProtectHome") {
		t.Fatalf("expected ProtectHome rejection, got %v\n%s", err, out.String())
	}
}

func TestBackupScheduleDisableRemovePassphraseFollowsDropInPath(t *testing.T) {
	host := t.TempDir()
	systemdDir := filepath.Join(host, "systemd")
	customPassPath := filepath.Join(host, "custom.passphrase")
	writeBackupServiceFixture(t, systemdDir, filepath.Join(host, "backup.passphrase"))
	restore := stubBackupScheduleSystemd(t, systemdDir, nil)
	defer restore()

	enable := NewRootCommand("test")
	enable.SetOut(new(bytes.Buffer))
	enable.SetErr(new(bytes.Buffer))
	enable.SetArgs([]string{
		"backup", "schedule", "enable",
		"--passphrase", "custom-path-passphrase",
		"--passphrase-path", customPassPath,
	})
	if err := enable.Execute(); err != nil {
		t.Fatalf("schedule enable: %v", err)
	}

	disable := NewRootCommand("test")
	disable.SetOut(new(bytes.Buffer))
	disable.SetErr(new(bytes.Buffer))
	disable.SetArgs([]string{"backup", "schedule", "disable", "--remove-passphrase"})
	if err := disable.Execute(); err != nil {
		t.Fatalf("schedule disable: %v", err)
	}
	if _, err := os.Stat(customPassPath); !os.IsNotExist(err) {
		t.Fatalf("custom passphrase file remains: %v", err)
	}
	if _, err := os.Stat(backupScheduleDropInPath(systemdDir)); !os.IsNotExist(err) {
		t.Fatalf("passphrase drop-in remains: %v", err)
	}
}

func stubBackupScheduleSystemd(t *testing.T, systemdDir string, hook func([]string) error) func() {
	t.Helper()
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldDir := backupSystemdDir
	oldRun := backupSystemctlRun
	backupSystemdDir = systemdDir
	backupSystemctlRun = func(args ...string) error {
		copied := append([]string(nil), args...)
		if hook != nil {
			if err := hook(copied); err != nil {
				return err
			}
		}
		return nil
	}
	return func() {
		backupSystemdDir = oldDir
		backupSystemctlRun = oldRun
	}
}

func writeBackupServiceFixture(t *testing.T, systemdDir, defaultPassphrasePath string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "packaging", "systemd", "veil-backup.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	text = strings.ReplaceAll(text, "/etc/veil/backup.passphrase", filepath.ToSlash(defaultPassphrasePath))
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "veil-backup.service"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return text
}

func runBackupCreateOneShot(t *testing.T, passphraseFile, statePath, keyPath, outputDir string) string {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--passphrase-file", passphraseFile,
		"--output-dir", outputDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("one-shot backup create: %v\n%s", err, out.String())
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	var archives []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".enc") {
			archives = append(archives, filepath.Join(outputDir, entry.Name()))
		}
	}
	if len(archives) != 1 {
		t.Fatalf("expected one archive in %s, got %v\n%s", outputDir, archives, out.String())
	}
	return archives[0]
}

func passphraseFileFromBackupService(t *testing.T, fragments ...string) string {
	t.Helper()
	execStart := effectiveSystemdDirective(fragments, "ExecStart")
	if execStart == "" {
		t.Fatal("effective veil-backup.service has no ExecStart")
	}
	path := passphraseFileFromExecStart(execStart)
	if path == "" {
		t.Fatalf("ExecStart missing --passphrase-file: %q", execStart)
	}
	return path
}

func sameBackupPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func TestBackupScheduleEnableRestoresPreviousPassphraseIfSystemdFails(t *testing.T) {
	oldPassphrase := "previous-passphrase"
	newPassphrase := "replacement-passphrase"

	t.Run("daemonReload", func(t *testing.T) {
		passPath, oldMode := writeScheduledPassphrase(t, oldPassphrase)
		restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), func(args []string) error {
			if len(args) > 0 && args[0] == "daemon-reload" {
				return fmt.Errorf("daemon-reload failed")
			}
			return nil
		})
		defer restore()

		err := runScheduleEnable(t, newPassphrase, passPath)
		if err == nil || !strings.Contains(err.Error(), "daemon-reload") {
			t.Fatalf("expected daemon-reload error, got %v", err)
		}
		assertPassphraseUnchanged(t, passPath, oldPassphrase, oldMode)
	})

	t.Run("timerEnable", func(t *testing.T) {
		passPath, oldMode := writeScheduledPassphrase(t, oldPassphrase)
		restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), func(args []string) error {
			if len(args) >= 1 && args[0] == "enable" {
				return fmt.Errorf("enable failed")
			}
			return nil
		})
		defer restore()

		err := runScheduleEnable(t, newPassphrase, passPath)
		if err == nil || !strings.Contains(err.Error(), "enable veil-backup.timer") {
			t.Fatalf("expected enable error, got %v", err)
		}
		assertPassphraseUnchanged(t, passPath, oldPassphrase, oldMode)
	})
}

func TestBackupScheduleEnableKeepsPreviousPassphraseIfPublicationFails(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "notadir")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "backup.passphrase")
	oldPassphrase := "previous-passphrase"
	if err := os.WriteFile(existing, []byte(oldPassphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}

	var calls int
	restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), func(args []string) error {
		calls++
		return nil
	})
	defer restore()

	err = runScheduleEnable(t, "replacement-passphrase", filepath.Join(parent, "backup.passphrase"))
	if err == nil {
		t.Fatal("expected passphrase publication error")
	}
	if calls != 0 {
		t.Fatalf("systemd ran despite publication failure: %d calls", calls)
	}
	assertPassphraseUnchanged(t, existing, oldPassphrase, info.Mode())
}

func TestBackupScheduleEnableRemovesNewPassphraseIfFreshEnableFails(t *testing.T) {
	passPath := filepath.Join(t.TempDir(), "backup.passphrase")
	restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), func(args []string) error {
		if len(args) > 0 && args[0] == "daemon-reload" {
			return fmt.Errorf("daemon-reload failed")
		}
		return nil
	})
	defer restore()

	err := runScheduleEnable(t, "replacement-passphrase", passPath)
	if err == nil || !strings.Contains(err.Error(), "daemon-reload") {
		t.Fatalf("expected daemon-reload error, got %v", err)
	}
	if _, err := os.Stat(passPath); !os.IsNotExist(err) {
		t.Fatalf("fresh failed enable left passphrase file: %v", err)
	}
	if _, err := os.Stat(passPath + ".replace-backup"); !os.IsNotExist(err) {
		t.Fatalf("replace-backup remains after failed fresh enable: %v", err)
	}
}

func TestBackupScheduleEnableCommitsNewPassphraseAfterSystemdSucceeds(t *testing.T) {
	passPath, _ := writeScheduledPassphrase(t, "previous-passphrase")
	restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), nil)
	defer restore()

	if err := runScheduleEnable(t, "replacement-passphrase", passPath); err != nil {
		t.Fatalf("schedule enable: %v", err)
	}
	got, err := os.ReadFile(passPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replacement-passphrase\n" {
		t.Fatalf("passphrase file = %q", got)
	}
	if _, err := os.Stat(passPath + ".replace-backup"); !os.IsNotExist(err) {
		t.Fatalf("replace-backup remains after success: %v", err)
	}
}

func TestBackupScheduleEnableFailedRotationKeepsRecoverableArchivePassphrase(t *testing.T) {
	oldPassphrase := "previous-passphrase"
	passPath, oldMode := writeScheduledPassphrase(t, oldPassphrase)
	statePath, keyPath := writeCLIBackupSource(t)
	archive := runBackupCreateOneShot(t, passPath, statePath, keyPath, t.TempDir())

	restore := stubBackupScheduleSystemd(t, filepath.Join(t.TempDir(), "systemd"), func(args []string) error {
		if len(args) > 0 && args[0] == "enable" {
			return fmt.Errorf("enable failed")
		}
		return nil
	})
	defer restore()

	err := runScheduleEnable(t, "replacement-passphrase", passPath)
	if err == nil || !strings.Contains(err.Error(), "enable veil-backup.timer") {
		t.Fatalf("expected enable error, got %v", err)
	}
	assertPassphraseUnchanged(t, passPath, oldPassphrase, oldMode)
	if _, err := backup.VerifyBackupFile(archive, oldPassphrase, 0); err != nil {
		t.Fatalf("existing archive no longer verifies with restored passphrase: %v", err)
	}
}

func writeScheduledPassphrase(t *testing.T, passphrase string) (string, os.FileMode) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.passphrase")
	if err := os.WriteFile(path, []byte(passphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, info.Mode()
}

func runScheduleEnable(t *testing.T, passphrase, passPath string) error {
	t.Helper()
	cmd := NewRootCommand("test")
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"backup", "schedule", "enable",
		"--passphrase", passphrase,
		"--passphrase-path", passPath,
	})
	return cmd.Execute()
}

func assertPassphraseUnchanged(t *testing.T, path, passphrase string, mode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read passphrase: %v", err)
	}
	if string(got) != passphrase+"\n" {
		t.Fatalf("passphrase file = %q, want %q", got, passphrase+"\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != mode {
		t.Fatalf("passphrase mode = %v, want %v", info.Mode(), mode)
	}
	if _, err := os.Stat(path + ".replace-backup"); !os.IsNotExist(err) {
		t.Fatalf("replace-backup remains: %v", err)
	}
}
