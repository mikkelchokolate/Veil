package cli

import "testing"

func TestResolveServeAuthTokenUsesFlagBeforeEnvironment(t *testing.T) {
	t.Setenv("VEIL_API_TOKEN", "env-token")

	token, source := resolveServeAuthToken("flag-token")

	if token != "flag-token" || source != "--auth-token" {
		t.Fatalf("expected flag token/source, got token=%q source=%q", token, source)
	}
}

func TestResolveServeAuthTokenUsesEnvironmentFallback(t *testing.T) {
	t.Setenv("VEIL_API_TOKEN", "env-token")

	token, source := resolveServeAuthToken("")

	if token != "env-token" || source != "VEIL_API_TOKEN" {
		t.Fatalf("expected env token/source, got token=%q source=%q", token, source)
	}
}

func TestResolveServeAuthTokenAllowsDisabledAuthForDevelopment(t *testing.T) {
	t.Setenv("VEIL_API_TOKEN", "")

	token, source := resolveServeAuthToken("")

	if token != "" || source != "disabled" {
		t.Fatalf("expected disabled auth, got token=%q source=%q", token, source)
	}
}
