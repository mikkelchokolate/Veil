package installer

import "testing"

func TestRURecommendedProfileModuleBuildsPanelInstallPolicy(t *testing.T) {
	profile, err := NewRURecommendedProfileModule(RURecommendedInput{
		Domain:       "vpn.example.com",
		Email:        "admin@example.com",
		Stack:        StackBoth,
		Port:         443,
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		PanelPort:    2096,
	}).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if profile.InstallNaive || profile.InstallHysteria2 || profile.InstallMieru || profile.Stack != StackPanel {
		t.Fatalf("stack policy should normalize to panel-only: %+v", profile)
	}
	if profile.PanelAuthToken != "secret-panel" || !profile.PanelTLSEnabled {
		t.Fatalf("panel credential/TLS policy = %+v", profile)
	}
	if profile.WebBasePath != "" {
		t.Fatalf("panel-only install should not generate Web base path without Caddy: %q", profile.WebBasePath)
	}
}
