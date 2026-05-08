package installer

import "testing"

func TestRURecommendedProfileModuleBuildsPanelInstallPolicy(t *testing.T) {
	profile, err := NewRURecommendedProfileModule(RURecommendedInput{
		Domain:    "vpn.example.com",
		Email:     "admin@example.com",
		Secret:    func(label string) string { return "secret-" + label },
		PanelPort: 2096,
	}).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if profile.InstallPanelCaddy {
		t.Fatalf("Veil install should default to direct/local Panel-only: %+v", profile)
	}
	if profile.PanelAuthToken != "secret-panel" || !profile.PanelTLSEnabled {
		t.Fatalf("panel credential/TLS policy = %+v", profile)
	}
	if profile.WebBasePath != "" {
		t.Fatalf("panel-only install should not generate Web base path without Caddy: %q", profile.WebBasePath)
	}
}
