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

	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	unit := managedFileContent(files, "veil.service")
	if !strings.Contains(unit, "ExecStart=/opt/veil/bin/veil serve") {
		t.Fatalf("veil.service should use selected binary path:\n%s", unit)
	}
}

func TestInstallApplyRendersCaddyUnitWithResolvedBinaryPath(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{InstallPanelCaddy: true, PanelAuthToken: "secret-panel", Caddyfile: "example.com { respond ok }"}
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd"), CaddyBinary: "/usr/bin/caddy"}

	files, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desiredFiles: %v", err)
	}
	unit := managedFileContent(files, "veil-naive.service")
	if !strings.Contains(unit, "ExecStart=/usr/bin/caddy run --config") || !strings.Contains(unit, "ExecReload=/usr/bin/caddy reload --config") {
		t.Fatalf("veil-naive.service should use resolved Caddy binary path:\n%s", unit)
	}
}

func managedFileContent(files []managedFile, name string) string {
	for _, file := range files {
		if filepath.Base(file.Path) == name {
			return file.Content
		}
	}
	return ""
}
