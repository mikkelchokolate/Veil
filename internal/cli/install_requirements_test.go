package cli

import (
	"strings"
	"testing"
)

func TestRURecommendedInstallRequirementsAllowPanelWithoutDomainOrPort(t *testing.T) {
	if err := NewRURecommendedInstallRequirements("panel").Validate(ruRecommendedInstallOptions{Stack: "panel"}); err != nil {
		t.Fatalf("panel install should not require domain/email/port: %v", err)
	}
}

func TestRURecommendedInstallRequirementsRequireDomainEmailForPanelCaddy(t *testing.T) {
	err := NewRURecommendedInstallRequirements("panel").Validate(ruRecommendedInstallOptions{Stack: "panel", PanelAccess: "caddy"})
	if err == nil || !strings.Contains(err.Error(), "--domain and --email are required for caddy Panel access") {
		t.Fatalf("missing panel caddy domain/email err = %v", err)
	}
	err = NewRURecommendedInstallRequirements("panel").Validate(ruRecommendedInstallOptions{Stack: "panel", PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com"})
	if err != nil {
		t.Fatalf("valid panel caddy requirements: %v", err)
	}
}
