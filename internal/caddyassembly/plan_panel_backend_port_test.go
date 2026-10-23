package caddyassembly

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Issue #1292 F6: an unparseable or out-of-range PanelListen used to become
// BackendPort 0. A panel-only bind then rendered a reverse proxy to
// 127.0.0.1:0, and a panel bind shared with naive silently dropped every
// panel route (the renderer skips BackendPort <= 0), so the panel vanished
// without an error. The plan must refuse instead.
func TestBuildRenderPlanRejectsInvalidPanelBackendPort(t *testing.T) {
	naive := []model.Inbound{{Name: "naive", Protocol: "naiveproxy", Enabled: true, Port: 443}}
	for _, listen := range []string{"", "127.0.0.1", "127.0.0.1:abc", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:-1"} {
		for _, tc := range []struct {
			name     string
			inbounds []model.Inbound
		}{{"panel-only", nil}, {"shared-with-naive", naive}} {
			t.Run(tc.name+"/"+listen, func(t *testing.T) {
				settings := model.Settings{PanelAccess: "caddy", PanelDomain: "panel.example.com", PanelEmail: "a@example.com", WebBasePath: "/p/", PanelListen: listen, Domain: "panel.example.com"}
				_, _, err := BuildRenderPlan(settings, tc.inbounds, nil)
				if err == nil || !strings.Contains(err.Error(), "panelListen") {
					t.Fatalf("BuildRenderPlan(PanelListen=%q) err = %v, want a panelListen error", listen, err)
				}
			})
		}
	}
}

func TestBuildRenderPlanRejectsOutOfRangePanelPublicPort(t *testing.T) {
	for _, port := range []int{-1, 65536} {
		settings := model.Settings{PanelAccess: "caddy", PanelDomain: "panel.example.com", PanelEmail: "a@example.com", WebBasePath: "/p/", PanelListen: "127.0.0.1:2096", PanelPublicPort: port}
		if _, _, err := BuildRenderPlan(settings, nil, nil); err == nil || !strings.Contains(err.Error(), "panelPublicPort") {
			t.Fatalf("BuildRenderPlan(PanelPublicPort=%d) err = %v, want a panelPublicPort error", port, err)
		}
	}
}

func TestBuildRenderPlanKeepsValidPanelBackendPort(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelDomain: "panel.example.com", PanelEmail: "a@example.com", WebBasePath: "/p/", PanelListen: "127.0.0.1:2096"}
	plan, _, err := BuildRenderPlan(settings, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range plan.Servers {
		if server.Kind == CaddyOwnerPanel && server.BackendPort == 2096 {
			return
		}
	}
	t.Fatalf("no panel server with BackendPort 2096: %+v", plan.Servers)
}
