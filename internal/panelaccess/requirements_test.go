package panelaccess

import (
	"testing"

	"github.com/veil-panel/veil/internal/model"
)

func TestCaddyRequirementDependsOnPanelAccessOrProtocolRequirement(t *testing.T) {
	policy := NewCaddyRequirement(func(protocol string) bool { return protocol == "naiveproxy" })
	if !policy.Required(model.Settings{PanelAccess: "caddy"}, nil) {
		t.Fatal("Panel caddy access should require Caddy")
	}
	if !policy.Required(model.Settings{}, []model.Inbound{{Protocol: "naiveproxy", Enabled: true}}) {
		t.Fatal("NaiveProxy inbound should require Caddy")
	}
	if policy.Required(model.Settings{}, []model.Inbound{{Protocol: "mieru", Enabled: true}}) {
		t.Fatal("Mieru inbound should not require Caddy")
	}
}

func TestNaiveCaddySettingsRequirement(t *testing.T) {
	err := NewNaiveCaddySettingsRequirement().Validate(model.Settings{Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil", NaivePassword: "secret"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	err = NewNaiveCaddySettingsRequirement().Validate(model.Settings{Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil"})
	if err == nil || err.Error() != "domain, email, naive username, and naive password are required for NaiveProxy/Caddy" {
		t.Fatalf("err = %v", err)
	}
}
