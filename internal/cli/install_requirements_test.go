package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestRURecommendedInstallRequirementsHasNoStackInterface(t *testing.T) {
	if _, ok := reflect.TypeOf(RURecommendedInstallRequirements{}).FieldByName("stack"); ok {
		t.Fatal("RURecommendedInstallRequirements should not carry legacy stack state")
	}
}

func TestRURecommendedInstallRequirementsAllowPanelWithoutDomainOrPort(t *testing.T) {
	if err := NewRURecommendedInstallRequirements().Validate(ruRecommendedInstallOptions{Stack: "panel"}); err != nil {
		t.Fatalf("panel install should not require domain/email/port: %v", err)
	}
}

func TestRURecommendedInstallRequirementsRequireDomainEmailForPanelCaddy(t *testing.T) {
	err := NewRURecommendedInstallRequirements().Validate(ruRecommendedInstallOptions{Stack: "panel", PanelAccess: "caddy"})
	if err == nil || !strings.Contains(err.Error(), "--domain and --email are required for caddy Panel access") {
		t.Fatalf("missing panel caddy domain/email err = %v", err)
	}
	err = NewRURecommendedInstallRequirements().Validate(ruRecommendedInstallOptions{Stack: "panel", PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com"})
	if err != nil {
		t.Fatalf("valid panel caddy requirements: %v", err)
	}
}
