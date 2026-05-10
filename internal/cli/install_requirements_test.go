package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestRURecommendedInstallOptionsDoNotExposeStack(t *testing.T) {
	if _, ok := reflect.TypeOf(ruRecommendedInstallOptions{}).FieldByName("Stack"); ok {
		t.Fatalf("ruRecommendedInstallOptions should not expose removed stack field")
	}
}

func TestRURecommendedInstallRequirementsAllowPanelWithoutDomainOrPort(t *testing.T) {
	if err := validateRURecommendedInstallRequirements(ruRecommendedInstallOptions{}); err != nil {
		t.Fatalf("panel install should not require domain/email/port: %v", err)
	}
}

func TestRURecommendedInstallRequirementsRequireDomainEmailForPanelCaddy(t *testing.T) {
	err := validateRURecommendedInstallRequirements(ruRecommendedInstallOptions{PanelAccess: "caddy"})
	if err == nil || !strings.Contains(err.Error(), "--domain and --email are required for caddy Panel access") {
		t.Fatalf("missing panel caddy domain/email err = %v", err)
	}
	err = validateRURecommendedInstallRequirements(ruRecommendedInstallOptions{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com"})
	if err != nil {
		t.Fatalf("valid panel caddy requirements: %v", err)
	}
}
