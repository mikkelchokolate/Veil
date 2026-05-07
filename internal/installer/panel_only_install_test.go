package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPanelOnlyInstallDoesNotRequireDomainAndWritesNoProxyConfigs(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Stack:      StackPanel,
		Secret:     func(label string) string { return "secret-" + label },
		PanelPort:  2096,
		RandomPort: func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile panel-only: %v", err)
	}
	if profile.InstallNaive || profile.InstallHysteria2 || profile.Caddyfile != "" || profile.Hysteria2YAML != "" || profile.WebBasePath != "" {
		t.Fatalf("panel-only profile should not include proxy artifacts: %+v", profile)
	}
	dir := t.TempDir()
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if _, err := os.Stat(result.CaddyfilePath); !os.IsNotExist(err) {
		t.Fatalf("panel-only install should not write Caddyfile, stat err: %v", err)
	}
	if _, err := os.Stat(result.Hysteria2Path); !os.IsNotExist(err) {
		t.Fatalf("panel-only install should not write Hysteria2 config, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil.service")); err != nil {
		t.Fatalf("panel-only install should write veil.service: %v", err)
	}
}
