package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestResolveRepairSystemdDir locks the repair side of #1484: an explicit flag
// wins, then the directory the installed veil.env records, then the packaged
// default — the same shape as the etc/var resolution next to it.
func TestResolveRepairSystemdDir(t *testing.T) {
	etcDir := t.TempDir()
	envPath := filepath.Join(etcDir, "veil.env")
	custom := filepath.Join(t.TempDir(), "systemd")
	if err := os.WriteFile(envPath, []byte("VEIL_SYSTEMD_DIR="+custom+"\n"), 0o640); err != nil {
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

	if got := resolveRepairSystemdDir(newCmd(custom), etcDir, custom); got != custom {
		t.Fatalf("explicit flag: got %q, want %q", got, custom)
	}
	if got := resolveRepairSystemdDir(newCmd(""), etcDir, defaultSystemdDir); got != custom {
		t.Fatalf("recorded directory: got %q, want %q", got, custom)
	}

	emptyEtc := t.TempDir()
	if got := resolveRepairSystemdDir(newCmd(""), emptyEtc, defaultSystemdDir); got != defaultSystemdDir {
		t.Fatalf("no environment: got %q, want %q", got, defaultSystemdDir)
	}
}

// TestRotateKeyPreflightScansTheRecordedSystemdDir keeps a custom --systemd-dir
// install safe: the stale pre-#1472 drop-in lives in the recorded directory, so
// the preflight must scan it instead of only the packaged one (#1484).
func TestRotateKeyPreflightScansTheRecordedSystemdDir(t *testing.T) {
	etcDir, keyPath, statePath, envPath := seedRotationFixture(t)
	t.Setenv("VEIL_ETC_DIR", etcDir)
	stubAdminSystemctl(t, envPath)

	customSystemd := t.TempDir()
	dropIn := writeHelperKeyPinDropIn(t, customSystemd, "[Service]\nEnvironment=VEIL_KEY_PATH="+filepath.Join(etcDir, "old-state.key")+"\n")
	env := "VEIL_KEY_PATH=" + keyPath + "\nVEIL_STATE_PATH=" + statePath + "\nVEIL_SYSTEMD_DIR=" + customSystemd + "\n"
	if err := os.WriteFile(envPath, []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	pointRotatePreflightAt(t, t.TempDir())

	out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath)
	if err == nil {
		t.Fatalf("rotate-key ignored a drop-in in the recorded unit directory:\n%s", out)
	}
	if !strings.Contains(err.Error(), dropIn) {
		t.Fatalf("error %v does not name %q", err, dropIn)
	}
}

// TestStaleKeyPinsIgnoreARelativeEnvironmentFile covers the #1496 review note:
// systemd ignores a relative EnvironmentFile=, so a same-named file in the CLI's
// working directory must not make the literal pin look harmless.
func TestStaleKeyPinsIgnoreARelativeEnvironmentFile(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	if err := os.WriteFile(filepath.Join(work, "veil.env"), []byte("VEIL_KEY_PATH=/elsewhere\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	systemdDir := t.TempDir()
	dir := filepath.Join(systemdDir, "veil-helper.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pinned := "/opt/veil/etc/state.key"
	body := bytes.NewBufferString("[Service]\nEnvironmentFile=veil.env\nEnvironment=VEIL_KEY_PATH=" + pinned + "\n").String()
	if err := os.WriteFile(filepath.Join(dir, "10-veil-install.conf"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	pins := staleKeyPins(systemdDir)
	if len(pins) != 1 || pins[0].Pinned != pinned {
		t.Fatalf("staleKeyPins = %+v, want the literal pin %q", pins, pinned)
	}
}
