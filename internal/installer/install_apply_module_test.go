package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallApplyModuleWritesManagedFiles(t *testing.T) {
	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc"), VarDir: filepath.Join(dir, "var")}
	profile := RURecommendedProfile{Domain: "vpn.example.com", InstallPanelCaddy: true, CaddyJSON: "{}"}

	result, err := NewInstallApply(profile, paths).Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Two managed-dir markers (.veil-managed under etc and var, #1145) plus
	// the caddy config and fallback index make up the written set.
	if len(result.WrittenFiles) != 4 {
		t.Fatalf("written files = %+v", result.WrittenFiles)
	}
	body, err := os.ReadFile(filepath.Join(paths.EtcDir, "generated", "caddy", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "{}" {
		t.Fatalf("caddy json = %q", string(body))
	}
}
