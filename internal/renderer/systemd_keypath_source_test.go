package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKeyPathComesOnlyFromTheEnvironmentFile locks #1471: the panel, the
// privileged helper and the scheduled backup must all resolve VEIL_KEY_PATH
// from the installed veil.env. A literal Environment= override or a
// --key-path flag wins over that file, so a rotate-key --new-key-path that
// repoints veil.env would leave those consumers on the pre-rotation key —
// scheduled backups then fail verification and key-based restore targets the
// wrong file.
func TestKeyPathComesOnlyFromTheEnvironmentFile(t *testing.T) {
	for _, tc := range []struct {
		unit string
		want string
	}{
		{"veil.service", "EnvironmentFile=-/etc/veil/veil.env"},
		{"veil-helper.service", "EnvironmentFile=-/etc/veil/veil.env"},
		{"veil-backup.service", "EnvironmentFile=-/etc/veil/veil.env"},
	} {
		body, err := os.ReadFile(filepath.Join("..", "..", "packaging", "systemd", tc.unit))
		if err != nil {
			t.Fatal(err)
		}
		unit := string(body)
		if strings.Contains(unit, "Environment=VEIL_KEY_PATH=") {
			t.Fatalf("%s hardcodes VEIL_KEY_PATH and would ignore a rotation (#1471):\n%s", tc.unit, unit)
		}
		if strings.Contains(unit, "--key-path") {
			t.Fatalf("%s passes --key-path, which wins over the environment file (#1471):\n%s", tc.unit, unit)
		}
		if !strings.Contains(unit, tc.want) {
			t.Fatalf("%s must read the installed environment file (%q):\n%s", tc.unit, tc.want, unit)
		}
	}

	// Same contract for a custom --etc-dir install, which renders its own
	// units and drop-ins instead of using the packaged files.
	units := RenderSystemdUnits(SystemdConfig{
		VeilBinary: "/usr/local/bin/veil",
		EtcDir:     "/opt/veil/etc",
		VarDir:     "/opt/veil/var",
	})
	for _, unit := range []string{UnitVeil, UnitHelperService, UnitBackupService} {
		if strings.Contains(units[unit], "Environment=VEIL_KEY_PATH=") || strings.Contains(units[unit], "--key-path") {
			t.Fatalf("rendered %s still pins the key path (#1471):\n%s", unit, units[unit])
		}
		if !strings.Contains(units[unit], "EnvironmentFile=-/opt/veil/etc/veil.env") {
			t.Fatalf("rendered %s must read the custom environment file:\n%s", unit, units[unit])
		}
	}
	helperDropIn, ok := RenderInstallDropIn(UnitHelperService, SystemdConfig{
		VeilBinary: "/usr/local/bin/veil",
		EtcDir:     "/opt/veil/etc",
		VarDir:     "/opt/veil/var",
	})
	if !ok {
		t.Fatal("helper drop-in missing")
	}
	if strings.Contains(helperDropIn, "Environment=VEIL_KEY_PATH=") {
		t.Fatalf("helper drop-in pins VEIL_KEY_PATH (#1471):\n%s", helperDropIn)
	}
	if !strings.Contains(helperDropIn, "EnvironmentFile=-/opt/veil/etc/veil.env") {
		t.Fatalf("helper drop-in must read the installed environment file (#1471):\n%s", helperDropIn)
	}
	backupDropIn, ok := RenderInstallDropIn(UnitBackupService, SystemdConfig{
		VeilBinary: "/usr/local/bin/veil",
		EtcDir:     "/opt/veil/etc",
		VarDir:     "/opt/veil/var",
	})
	if !ok {
		t.Fatal("backup drop-in missing")
	}
	if strings.Contains(backupDropIn, "--key-path") {
		t.Fatalf("backup drop-in passes --key-path (#1471):\n%s", backupDropIn)
	}
}
