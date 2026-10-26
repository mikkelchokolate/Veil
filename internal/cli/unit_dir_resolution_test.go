package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uninstallflow "github.com/mikkelchokolate/Veil/internal/cliflow/uninstall"
	"github.com/spf13/cobra"
)

// TestResolveSystemdUnitDir locks the shared resolution order: an explicit
// --systemd-dir wins, then the directory recorded in veil.env, then the value
// the command already had (#1505).
func TestResolveSystemdUnitDir(t *testing.T) {
	etcDir := t.TempDir()
	envPath := filepath.Join(etcDir, "veil.env")
	recorded := filepath.Join(t.TempDir(), "systemd")
	if err := os.WriteFile(envPath, []byte("VEIL_SYSTEMD_DIR="+recorded+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	newCmd := func(set string) *cobra.Command {
		cmd := &cobra.Command{}
		value := defaultSystemdDir
		cmd.Flags().StringVar(&value, "systemd-dir", defaultSystemdDir, "")
		if set != "" {
			if err := cmd.Flags().Set("systemd-dir", set); err != nil {
				t.Fatal(err)
			}
		}
		return cmd
	}

	if got := resolveSystemdUnitDir(newCmd("/opt/explicit"), envPath, "/opt/explicit", defaultSystemdDir); got != "/opt/explicit" {
		t.Fatalf("explicit flag: got %q", got)
	}
	if got := resolveSystemdUnitDir(newCmd(""), envPath, defaultSystemdDir, defaultSystemdDir); got != recorded {
		t.Fatalf("recorded directory: got %q, want %q", got, recorded)
	}
	emptyEnv := filepath.Join(t.TempDir(), "veil.env")
	if got := resolveSystemdUnitDir(newCmd(""), emptyEnv, defaultSystemdDir, defaultSystemdDir); got != defaultSystemdDir {
		t.Fatalf("packaged fallback: got %q", got)
	}
	if got := resolveSystemdUnitDir(nil, emptyEnv, "/opt/seam", defaultSystemdDir); got != "/opt/seam" {
		t.Fatalf("programmatic seam: got %q", got)
	}
}

// TestUninstallUsesTheRecordedSystemdDir proves the command hands the recorded
// unit tree to the uninstall flow instead of the packaged one.
func TestUninstallUsesTheRecordedSystemdDir(t *testing.T) {
	etcDir := t.TempDir()
	custom := filepath.Join(t.TempDir(), "systemd")
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_SYSTEMD_DIR="+custom+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	original := uninstallRunFunc
	var got uninstallflow.Options
	uninstallRunFunc = func(opts uninstallflow.Options, _ io.Writer, _ io.Writer, _ uninstallflow.Dependencies) error {
		got = opts
		return nil
	}
	t.Cleanup(func() { uninstallRunFunc = original })
	_ = got

	originalStopper := uninstallServiceStopper
	uninstallServiceStopper = func(string) error { return nil }
	t.Cleanup(func() { uninstallServiceStopper = originalStopper })

	cmd := newFastRootCommand("test")
	cmd.SetArgs([]string{"uninstall", "--dry-run", "--etc-dir", etcDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if got.SystemdDir != custom {
		t.Fatalf("uninstall SystemdDir = %q, want the recorded %q", got.SystemdDir, custom)
	}
}

// TestRepairDiscoveryDirUsesTheFlag keeps the layout discovery on the tree the
// operator named.
func TestRepairDiscoveryDirUsesTheFlag(t *testing.T) {
	cmd := &cobra.Command{}
	value := ""
	cmd.Flags().StringVar(&value, "systemd-dir", "", "")
	if got := repairDiscoveryDir(cmd, "/opt/veil/systemd"); got != repairDiscoverySystemdDir {
		t.Fatalf("unchanged flag: got %q", got)
	}
	if err := cmd.Flags().Set("systemd-dir", "/opt/veil/systemd"); err != nil {
		t.Fatal(err)
	}
	if got := repairDiscoveryDir(cmd, "/opt/veil/systemd"); got != "/opt/veil/systemd" {
		t.Fatalf("explicit flag: got %q", got)
	}
}

// TestBackupScheduleWritesIntoTheRecordedSystemdDir keeps the timer drop-in on
// the installed unit tree.
func TestBackupScheduleWritesIntoTheRecordedSystemdDir(t *testing.T) {
	etcDir := t.TempDir()
	custom := t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_SYSTEMD_DIR="+custom+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	originalDir := backupSystemdDir
	originalSystemctl := backupSystemctlRun
	t.Cleanup(func() {
		backupSystemdDir = originalDir
		backupSystemctlRun = originalSystemctl
	})
	backupSystemdDir = defaultSystemdDir
	var calls []string
	backupSystemctlRun = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	passphrasePath := filepath.Join(t.TempDir(), "pass")

	cmd := newFastRootCommand("test")
	cmd.SetArgs([]string{"backup", "schedule", "enable", "--passphrase", "0123456789abcdef0123456789abcdef", "--passphrase-path", passphrasePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("backup schedule enable: %v", err)
	}
	dropIn := backupScheduleDropInPath(custom)
	if _, err := os.Stat(dropIn); err != nil {
		t.Fatalf("schedule drop-in missing from the recorded unit dir %s: %v (systemd calls: %v)", custom, err, calls)
	}
}
