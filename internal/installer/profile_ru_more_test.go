package installer

import (
	"fmt"
	"testing"
)

func TestBuildRURecommendedInstallUsesDefaultRandomPanelPort(t *testing.T) {
	install, err := BuildRURecommendedInstall(RURecommendedInstallInput{
		Secret: func(label string) string { return "secret" },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !install.PanelRandom {
		t.Fatalf("expected random panel port selection")
	}
	if install.PanelPort < RandomPortMin || install.PanelPort > RandomPortMax {
		t.Fatalf("port %d outside expected range [%d,%d]", install.PanelPort, RandomPortMin, RandomPortMax)
	}
}

func TestBuildRURecommendedInstallPropagatesPortSelectionError(t *testing.T) {
	sentinel := fmt.Errorf("port selection failure")
	_, err := BuildRURecommendedInstall(RURecommendedInstallInput{
		PanelPort:       0,
		RandomPanelPort: func() (int, error) { return 0, sentinel },
		Secret:          func(label string) string { return "secret" },
	})
	if err != sentinel {
		t.Fatalf("expected sentinel error %v, got %v", sentinel, err)
	}
}

func TestBuildRURecommendedInstallPropagatesProfileBuildError(t *testing.T) {
	_, err := BuildRURecommendedInstall(RURecommendedInstallInput{
		PanelAccess: "caddy",
		Email:       "admin@example.com",
		Secret:      func(label string) string { return "secret" },
	})
	if err == nil {
		t.Fatalf("expected profile build error")
	}
}

// TestBuildRURecommendedProfileFailsWhenRandomSuffixFails is the #1022
// regression: a crypto/rand failure must abort the build, not degrade to the
// known "admin_admin" credential.
func TestBuildRURecommendedProfileFailsWhenRandomSuffixFails(t *testing.T) {
	orig := randomReader
	defer func() { randomReader = orig }()
	sentinel := fmt.Errorf("random suffix failure")
	randomReader = func(b []byte) (int, error) {
		if len(b) == 2 {
			return 0, sentinel
		}
		return orig(b)
	}

	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "local",
		Secret:      func(label string) string { return "secret" },
	})
	if err == nil {
		t.Fatalf("expected error, got profile %+v", profile)
	}
	if profile.Username == "admin_admin" {
		t.Fatalf("known-credential fallback must never be installed: %+v", profile)
	}
}

// TestBuildRURecommendedProfileFailsWhenRandomPasswordFails is the #1022
// regression: a crypto/rand failure must abort the build, not degrade to the
// known "change-me" password.
func TestBuildRURecommendedProfileFailsWhenRandomPasswordFails(t *testing.T) {
	orig := randomReader
	defer func() { randomReader = orig }()
	sentinel := fmt.Errorf("random password failure")
	randomReader = func(b []byte) (int, error) {
		if len(b) == 8 {
			return 0, sentinel
		}
		return orig(b)
	}

	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "local",
		Secret:      func(label string) string { return "secret" },
	})
	if err == nil {
		t.Fatalf("expected error, got profile %+v", profile)
	}
	if profile.Password == "change-me" {
		t.Fatalf("known-credential fallback must never be installed: %+v", profile)
	}
}

// TestBuildRURecommendedProfileFailsWhenSecretReturnsEmpty is the #1022
// regression: an empty token is the generator's fail-closed signal.
func TestBuildRURecommendedProfileFailsWhenSecretReturnsEmpty(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "local",
		Secret:      func(label string) string { return "" },
	})
	if err == nil {
		t.Fatalf("expected error, got profile %+v", profile)
	}
	if profile.PanelAuthToken != "" {
		t.Fatalf("expected no token on failure, got %q", profile.PanelAuthToken)
	}
}

func TestNormalizedInputProvidesDefaultSecret(t *testing.T) {
	module := NewRURecommendedProfileModule(RURecommendedInput{})
	profile, err := module.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.PanelAuthToken != "panel" {
		t.Fatalf("expected default secret identity to return %q, got %q", "panel", profile.PanelAuthToken)
	}
}

func TestGenerateRandomHexPropagatesReaderError(t *testing.T) {
	orig := randomReader
	defer func() { randomReader = orig }()

	sentinel := fmt.Errorf("random reader failure")
	randomReader = func([]byte) (int, error) { return 0, sentinel }

	_, err := generateRandomHex(4)
	if err != sentinel {
		t.Fatalf("expected sentinel error %v, got %v", sentinel, err)
	}
}
