package installer

import "testing"

func TestRURecommendedProfileModuleBuildsInstallPolicy(t *testing.T) {
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
	if !profile.InstallNaive || !profile.InstallHysteria2 || profile.Stack != StackBoth {
		t.Fatalf("stack policy = %+v", profile)
	}
	if profile.NaivePassword != "secret-naive" || profile.Hysteria2Password != "secret-hysteria2" || profile.PanelAuthToken != "secret-panel" {
		t.Fatalf("credential policy = %+v", profile)
	}
	if profile.WebBasePath == "" || profile.WebBasePath[0] != '/' || profile.WebBasePath[len(profile.WebBasePath)-1] != '/' {
		t.Fatalf("web base path = %q", profile.WebBasePath)
	}
}
