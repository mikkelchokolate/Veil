package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallApplyModuleWritesManagedFiles(t *testing.T) {
	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc"), VarDir: filepath.Join(dir, "var")}
	profile := RURecommendedProfile{Domain: "vpn.example.com", InstallPanelCaddy: true, Caddyfile: "caddy"}

	result, err := NewInstallApply(profile, paths).Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.WrittenFiles) != 2 {
		t.Fatalf("written files = %+v", result.WrittenFiles)
	}
	body, err := os.ReadFile(filepath.Join(paths.EtcDir, "generated", "caddy", "panel.Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "caddy" {
		t.Fatalf("caddyfile = %q", string(body))
	}
}
