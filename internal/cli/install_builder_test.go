package cli

import "testing"

func TestBuildRURecommendedInstallFromOptionsHonorsExplicitPanelPort(t *testing.T) {
	install, err := buildRURecommendedInstallFromOptions(ruRecommendedInstallOptions{
		Stack:     "both",
		Domain:    "example.com",
		Email:     "admin@example.com",
		PanelPort: 2096,
	})
	if err != nil {
		t.Fatalf("buildRURecommendedInstallFromOptions: %v", err)
	}
	if install.Profile.Domain != "example.com" || install.Profile.Email != "admin@example.com" {
		t.Fatalf("unexpected profile: %+v", install.Profile)
	}
	if install.PanelPort != 2096 || install.PanelRandom {
		t.Fatalf("expected explicit panel port, got port=%d random=%v", install.PanelPort, install.PanelRandom)
	}
}
