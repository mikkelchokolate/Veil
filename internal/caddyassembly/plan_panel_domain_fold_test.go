package caddyassembly

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// Issue #1291: the panel server's Domain feeds Caddy host matchers and TLS
// subjects verbatim, so it must be folded like domain.go's ACME email map —
// otherwise legacy mixed-case/padded values render a matcher and a cert
// subject that disagree with the ACME policy keyed on the folded name.
func TestBuildRenderPlanFoldsPanelDomain(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings model.Settings
	}{
		{"panelDomain", model.Settings{PanelAccess: "caddy", PanelDomain: " Panel.Example.COM ", PanelEmail: "admin@example.com", WebBasePath: "/p/", PanelListen: "127.0.0.1:2096"}},
		{"domain fallback", model.Settings{PanelAccess: "caddy", Domain: "Panel.EXAMPLE.com ", Email: "admin@example.com", WebBasePath: "/p/", PanelListen: "127.0.0.1:2096"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, _, err := BuildRenderPlan(tc.settings, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
			server, ok := plan.Servers[key]
			if !ok || server.Kind != CaddyOwnerPanel {
				t.Fatalf("no panel server on %v: %+v", key, plan.Servers)
			}
			if server.Domain != "panel.example.com" {
				t.Fatalf("panel server Domain = %q, want panel.example.com", server.Domain)
			}
		})
	}
}
