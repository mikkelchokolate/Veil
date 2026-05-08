package installer

import (
	"strings"
	"testing"
)

func TestBuildRURecommendedProfileDefaultsToPanelOnly(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Stack != StackPanel || profile.InstallNaive || profile.InstallHysteria2 || profile.InstallMieru {
		t.Fatalf("expected panel-only profile, got %+v", profile)
	}
	if profile.PortPlan.Port != 0 || profile.Caddyfile != "" || profile.Hysteria2YAML != "" || profile.NaiveClientURL != "" || profile.Hysteria2ClientURI != "" {
		t.Fatalf("panel-only profile should not include protocol artifacts: %+v", profile)
	}
	if profile.PanelAuthToken != "secret-panel" || !profile.PanelTLSEnabled {
		t.Fatalf("panel credentials/TLS not wired into profile: %+v", profile)
	}
}

func TestBuildRURecommendedProfileDoesNotPlanSharedProxyPortForLegacyStacks(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: StackBoth, Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("legacy stack should not check shared proxy port availability: %v", err)
	}
	if profile.PortPlan.Port != 0 || profile.PortPlan.Reason != "" {
		t.Fatalf("legacy stack should not create shared proxy port plan: %+v", profile.PortPlan)
	}
}

func TestBuildRURecommendedProfileNormalizesLegacyProtocolStacksToPanelOnly(t *testing.T) {
	for _, stack := range []Stack{"", StackBoth, StackNaive, StackHysteria2, StackMieru} {
		profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: stack, Secret: func(label string) string { return "secret-" + label }})
		if err != nil {
			t.Fatalf("BuildRURecommendedProfile(%q): %v", stack, err)
		}
		if profile.Stack != StackPanel || profile.InstallNaive || profile.InstallHysteria2 || profile.InstallMieru || profile.PortPlan.Port != 0 {
			t.Fatalf("legacy stack %q should normalize to panel-only, got %+v", stack, profile)
		}
	}
}

func TestNormalizeStackTrimsWhitespaceAndNormalizesLegacyStacksToPanel(t *testing.T) {
	tests := []struct {
		name    string
		input   Stack
		wantErr bool
	}{
		{"empty", "", false},
		{"both exact", StackBoth, false},
		{"both with spaces", " both ", false},
		{"naive with spaces", " naive ", false},
		{"hysteria2 with spaces", " hysteria2 ", false},
		{"mieru with spaces", " mieru ", false},
		{"invalid", "bogus", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStack, gotNaive, gotHy2, err := normalizeStack(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if tt.wantErr {
				return
			}
			if gotStack != StackPanel || gotNaive || gotHy2 {
				t.Fatalf("normalizeStack(%q) = %q naive=%v hy2=%v, want panel false false", tt.input, gotStack, gotNaive, gotHy2)
			}
		})
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
	if !strings.Contains(profile.Caddyfile, "handle_path /"+strings.Trim(profile.WebBasePath, "/")+"/*") {
		t.Fatalf("Caddyfile missing handle_path for web base path %s:\n%s", profile.WebBasePath, profile.Caddyfile)
	}
}

func TestBuildRURecommendedProfileRejectsPanelCaddyWhenPanelPortZero(t *testing.T) {
	_, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com", Secret: func(label string) string { return "secret-" + label }, PanelPort: 0})
	if err == nil {
		t.Fatalf("expected Panel Caddy access to require selected Panel port")
	}
}
