package installer

import (
	"strings"
	"testing"
)

func TestBuildRURecommendedProfileCreatesSamePortConfigsAndLinks(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain: "example.com",
		Email: "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret: func(label string) string { return "secret-" + label },
		RandomPort: func() int { return 31874 },
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
}

func TestBuildRURecommendedProfileRejectsMissingDomain(t *testing.T) {
	_, err := BuildRURecommendedProfile(RURecommendedInput{
		Email: "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret: func(label string) string { return "secret" },
		RandomPort: func() int { return 31874 },
	})
	if err == nil {
		t.Fatalf("expected missing domain error")
	}
}
