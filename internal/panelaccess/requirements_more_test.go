package panelaccess

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestCaddyRequirementNilFunc(t *testing.T) {
	req := NewCaddyRequirement(nil)
	if req.Required(model.Settings{}, []model.Inbound{{Protocol: "naiveproxy", Enabled: true}}) {
		t.Fatal("nil requiresCaddy should not require Caddy")
	}
}

func TestCaddyRequirementDisabledInbound(t *testing.T) {
	req := NewCaddyRequirement(func(protocol string) bool { return protocol == "naiveproxy" })
	if req.Required(model.Settings{}, []model.Inbound{{Protocol: "naiveproxy", Enabled: false}}) {
		t.Fatal("disabled inbound should not require Caddy")
	}
}
