package cli

import (
	"strings"
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
	err := validateServeAuthBinding("0.0.0.0:2096", "disabled")
	if err == nil {
		t.Fatalf("expected public listen without auth token to be rejected")
	}
	if !strings.Contains(err.Error(), "--auth-token") || !strings.Contains(err.Error(), "VEIL_API_TOKEN") {
		t.Fatalf("expected auth error to explain token sources, got %v", err)
	}
}

func TestValidateServeAuthBindingRejectsInvalidPortOnLocalhost(t *testing.T) {
	err := validateServeAuthBinding("localhost:notaport", "disabled")
	if err == nil {
		t.Fatalf("expected invalid port on localhost to be rejected")
	}
	if !strings.Contains(err.Error(), "host:port") && !strings.Contains(err.Error(), "listen address") && !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("expected error to mention host:port/listen address or invalid port, got %v", err)
	}
}

func TestValidateServeAuthBindingAllowsDisabledAuthOnLocalhost(t *testing.T) {
	for _, listen := range []string{"localhost:2096", "LOCALHOST:2096", "127.0.0.1:2096", "[::1]:2096"} {
		t.Run(listen, func(t *testing.T) {
			if err := validateServeAuthBinding(listen, "disabled"); err != nil {
				t.Fatalf("expected local listen without auth token to be allowed: %v", err)
			}
		})
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
