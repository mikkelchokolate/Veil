package panelaccess

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
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
