package cli

import (
	"testing"
	"time"
)

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

func TestValidateServeAuthBindingRejectsDisabledAuthOnPublicListen(t *testing.T) {
	if err := validateServeAuthBinding("0.0.0.0:2096", "disabled"); err == nil {
		t.Fatalf("expected public listen without auth token to be rejected")
	}
}

func TestNewServeHTTPServerSetsProductionTimeouts(t *testing.T) {
	server := newServeHTTPServer("127.0.0.1:2096", "test", "token", "/tmp/state.json", "/tmp/apply")

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("unexpected ReadHeaderTimeout: %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("unexpected ReadTimeout: %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 120*time.Second {
		t.Fatalf("unexpected WriteTimeout: %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 120*time.Second {
		t.Fatalf("unexpected IdleTimeout: %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unexpected MaxHeaderBytes: %d", server.MaxHeaderBytes)
	}
}
