package installer

import (
	"strings"
	"testing"
)

func TestBuildRURecommendedProfileCreatesSamePortConfigsAndLinks(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.PortPlan.Port != 443 {
		t.Fatalf("expected shared port 443, got %d", profile.PortPlan.Port)
	}
	if !strings.Contains(profile.Caddyfile, ":443, example.com") {
		t.Fatalf("expected Caddyfile for port/domain:\n%s", profile.Caddyfile)
	}
	if !strings.Contains(profile.Hysteria2YAML, "listen: :443") {
		t.Fatalf("expected Hysteria2 listen port:\n%s", profile.Hysteria2YAML)
	}
	if !strings.Contains(profile.NaiveClientURL, "https://veil:secret-naive@example.com:443") {
		t.Fatalf("bad naive url: %s", profile.NaiveClientURL)
	}
	if !strings.Contains(profile.Hysteria2ClientURI, "hysteria2://secret-hysteria2@example.com:443") {
		t.Fatalf("bad hysteria2 uri: %s", profile.Hysteria2ClientURI)
	}
	if profile.PanelAuthToken != "secret-panel" {
		t.Fatalf("panel auth token not wired into profile: %+v", profile)
	}
}

func TestBuildRURecommendedProfileSupportsNaiveOnly(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        StackNaive,
		Availability: PortAvailability{UDPBusy: map[int]bool{443: true}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !profile.InstallNaive || profile.InstallHysteria2 {
		t.Fatalf("unexpected stack flags: naive=%v hy2=%v", profile.InstallNaive, profile.InstallHysteria2)
	}
	if profile.PortPlan.Port != 443 {
		t.Fatalf("expected naive-only profile to ignore busy UDP/443, got %d", profile.PortPlan.Port)
	}
	if profile.Caddyfile == "" || profile.NaiveClientURL == "" {
		t.Fatalf("expected naive config and link")
	}
	if profile.Hysteria2YAML != "" || profile.Hysteria2ClientURI != "" {
		t.Fatalf("did not expect hysteria config/link")
	}
}

func TestBuildRURecommendedProfileSupportsHysteriaOnly(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        StackHysteria2,
		Availability: PortAvailability{TCPBusy: map[int]bool{443: true}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.InstallNaive || !profile.InstallHysteria2 {
		t.Fatalf("unexpected stack flags: naive=%v hy2=%v", profile.InstallNaive, profile.InstallHysteria2)
	}
	if profile.PortPlan.Port != 443 {
		t.Fatalf("expected hysteria2-only profile to ignore busy TCP/443, got %d", profile.PortPlan.Port)
	}
	if profile.Caddyfile != "" || profile.NaiveClientURL != "" {
		t.Fatalf("did not expect naive config/link")
	}
	if profile.Hysteria2YAML == "" || profile.Hysteria2ClientURI == "" {
		t.Fatalf("expected hysteria config and link")
	}
}

func TestNormalizeStackTrimsWhitespace(t *testing.T) {
	tests := []struct {
		name               string
		input              Stack
		wantStack          Stack
		wantNaive          bool
		wantHysteria2      bool
		wantErr            bool
	}{
		{"empty", "", StackBoth, true, true, false},
		{"both exact", StackBoth, StackBoth, true, true, false},
		{"both with spaces", " both ", StackBoth, true, true, false},
		{"naive with spaces", " naive ", StackNaive, true, false, false},
		{"hysteria2 with spaces", " hysteria2 ", StackHysteria2, false, true, false},
		{"invalid", "bogus", "", false, false, true},
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
			if gotStack != tt.wantStack {
				t.Errorf("stack = %q, want %q", gotStack, tt.wantStack)
			}
			if gotNaive != tt.wantNaive {
				t.Errorf("installNaive = %v, want %v", gotNaive, tt.wantNaive)
			}
			if gotHy2 != tt.wantHysteria2 {
				t.Errorf("installHysteria2 = %v, want %v", gotHy2, tt.wantHysteria2)
			}
		})
	}
}

func TestBuildRURecommendedProfileRejectsMissingDomain(t *testing.T) {
	_, err := BuildRURecommendedProfile(RURecommendedInput{
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret" },
		RandomPort:   func() int { return 31874 },
	})
	if err == nil {
		t.Fatalf("expected missing domain error")
	}
}

func TestBuildRURecommendedProfileGeneratesWebBasePath(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.WebBasePath == "" {
		t.Fatalf("expected WebBasePath to be generated")
	}
	if profile.WebBasePath == "/" {
		t.Fatalf("expected WebBasePath to be a random path, not /")
	}
	if profile.WebBasePath[0] != '/' || profile.WebBasePath[len(profile.WebBasePath)-1] != '/' {
		t.Fatalf("WebBasePath must start and end with /, got: %s", profile.WebBasePath)
	}
	// 9 random bytes → 12 base64url chars + 2 slashes = 14 chars
	if len(profile.WebBasePath) != 14 {
		t.Fatalf("expected WebBasePath length 14 (/ + 12 base64url + /), got %d: %s", len(profile.WebBasePath), profile.WebBasePath)
	}
}

func TestBuildRURecommendedProfileIncludesPanelReverseProxyInCaddyfile(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
		PanelPort:    2096,
	})
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

func TestBuildRURecommendedProfileNoPanelReverseProxyWhenPanelPortZero(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
		PanelPort:    0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(profile.Caddyfile, "reverse_proxy") {
		t.Fatalf("Caddyfile should not contain reverse_proxy when PanelPort is 0:\n%s", profile.Caddyfile)
	}
}
