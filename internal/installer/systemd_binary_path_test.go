package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallApplyRendersVeilUnitWithSelectedBinaryPath(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd"), VeilBinary: "/opt/veil/bin/veil"}

	files, err := NewManagedFileRepair(profile, paths).desiredFiles()
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	unit := ""
	for _, file := range files {
		if filepath.Base(file.Path) == "veil.service" {
			unit = file.Content
		}
	}
	if !strings.Contains(unit, "ExecStart=/opt/veil/bin/veil serve") {
		t.Fatalf("veil.service should use selected binary path:\n%s", unit)
	}
}
