package installer

import (
	"strings"
	"testing"
)

func TestBuildRURecommendedProfileDefaultsPanelAccessLocal(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret" }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if profile.PanelAccess != "local" || profile.PanelListen != "127.0.0.1:2096" {
		t.Fatalf("default Panel access = %q listen=%q, want local 127.0.0.1:2096", profile.PanelAccess, profile.PanelListen)
	}
}

func TestBuildRURecommendedProfileDefaultsToPanelOnly(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.InstallPanelCaddy {
		t.Fatalf("expected direct/local panel-only profile, got %+v", profile)
	}
	if profile.Caddyfile != "" {
		t.Fatalf("panel-only profile should not include protocol artifacts: %+v", profile)
	}
	if profile.PanelAuthToken != "secret-panel" || !profile.PanelTLSEnabled {
		t.Fatalf("panel credentials/TLS not wired into profile: %+v", profile)
	}
}

func TestBuildRURecommendedProfileRejectsMissingDomainForPanelCaddyAccess(t *testing.T) {
	_, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Email: "admin@example.com", Secret: func(label string) string { return "secret" }})
	if err == nil {
		t.Fatalf("expected missing domain error")
	}
}

func TestBuildRURecommendedProfileGeneratesWebBasePathForPanelCaddyAccess(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com", Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.WebBasePath == "" || profile.WebBasePath == "/" {
		t.Fatalf("expected random WebBasePath, got %q", profile.WebBasePath)
	}
	if profile.WebBasePath[0] != '/' || profile.WebBasePath[len(profile.WebBasePath)-1] != '/' {
		t.Fatalf("WebBasePath must start and end with /, got: %s", profile.WebBasePath)
	}
	if len(profile.WebBasePath) != 14 {
		t.Fatalf("expected WebBasePath length 14 (/ + 12 base64url + /), got %d: %s", len(profile.WebBasePath), profile.WebBasePath)
	}
}

func TestBuildRURecommendedProfileIncludesPanelReverseProxyInCaddyfile(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com", Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(profile.Caddyfile, "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("Caddyfile missing reverse_proxy for panel port:\n%s", profile.Caddyfile)
	}
	if !strings.Contains(profile.Caddyfile, "handle /"+strings.Trim(profile.WebBasePath, "/")+"/*") {
		t.Fatalf("Caddyfile missing handle_path for web base path %s:\n%s", profile.WebBasePath, profile.Caddyfile)
	}
}

func TestBuildRURecommendedProfileRejectsPanelCaddyWhenPanelPortZero(t *testing.T) {
	_, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com", Secret: func(label string) string { return "secret-" + label }, PanelPort: 0})
	if err == nil {
		t.Fatalf("expected Panel Caddy access to require selected Panel port")
	}
}
