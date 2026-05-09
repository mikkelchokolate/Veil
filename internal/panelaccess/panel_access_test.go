package panelaccess

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/model"
	"github.com/veil-panel/veil/internal/renderer"
)

func TestPanelAccessBuildsCaddyRouteConfigAndApplyIntent(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	route, ok, err := access.CaddyRoute()
	if err != nil || !ok {
		t.Fatalf("CaddyRoute ok=%v err=%v", ok, err)
	}
	if route.Port != 2096 || route.WebBasePath != "/panel-secret/" {
		t.Fatalf("route = %+v", route)
	}
	artifact, ok, err := access.GeneratedConfig(generatedconfig.NewPaths("/etc/veil"))
	if err != nil || !ok {
		t.Fatalf("GeneratedConfig ok=%v err=%v", ok, err)
	}
	if artifact.Path != "/etc/veil/generated/caddy/Caddyfile" || !strings.Contains(artifact.Body, "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("artifact = %+v", artifact)
	}
	intent := access.ApplyIntent([]model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}})
	if !contains(intent.Configs, "/etc/veil/generated/caddy/Caddyfile") || !contains(intent.Actions, "reload "+renderer.UnitNaive) || !contains(intent.Runtimes, renderer.UnitNaive) {
		t.Fatalf("intent = %+v", intent)
	}
	if len(intent.Errors) != 1 || !strings.Contains(intent.Errors[0], "panel caddy access uses 443/tcp") {
		t.Fatalf("intent errors = %+v", intent.Errors)
	}
}

func TestPanelAccessSkipsNonCaddyMode(t *testing.T) {
	access := New(model.Settings{PanelAccess: "local", PanelListen: "127.0.0.1:2096"}, nil)
	if _, ok, err := access.CaddyRoute(); err != nil || ok {
		t.Fatalf("CaddyRoute ok=%v err=%v", ok, err)
	}
	if _, ok, err := access.GeneratedConfig(generatedconfig.NewPaths("/etc/veil")); err != nil || ok {
		t.Fatalf("GeneratedConfig ok=%v err=%v", ok, err)
	}
	if intent := access.ApplyIntent(nil); len(intent.Configs) != 0 || len(intent.Actions) != 0 || len(intent.Runtimes) != 0 || len(intent.Errors) != 0 {
		t.Fatalf("intent = %+v", intent)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
