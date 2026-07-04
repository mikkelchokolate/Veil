package panelaccess

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestCaddyRouteRejectsInvalidConfiguration(t *testing.T) {
	requiresCaddy := func(protocol string) bool { return protocol == "naiveproxy" }
	for _, tc := range []struct {
		name        string
		settings    model.Settings
		wantOk      bool
		wantErrText string
	}{
		{
			name:     "non-caddy mode returns not ok without error",
			settings: model.Settings{PanelAccess: "local", PanelListen: "127.0.0.1:2096"},
			wantOk:   false,
		},
		{
			name:        "missing web base path",
			settings:    model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com"},
			wantOk:      false,
			wantErrText: "webBasePath is required",
		},
		{
			name:        "panel listen missing port",
			settings:    model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel"},
			wantOk:      false,
			wantErrText: "panelListen must be host:port",
		},
		{
			name:        "panel listen with non-numeric port",
			settings:    model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:abc", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel"},
			wantOk:      false,
			wantErrText: "panelListen must be host:port",
		},
		{
			name:        "panel listen with port zero",
			settings:    model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:0", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel"},
			wantOk:      false,
			wantErrText: "panelListen must be host:port",
		},
		{
			name:        "panel listen with port out of range",
			settings:    model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:70000", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel"},
			wantOk:      false,
			wantErrText: "panelListen must be host:port",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := New(tc.settings, requiresCaddy)
			route, ok, err := access.CaddyRoute()
			if ok != tc.wantOk {
				t.Fatalf("CaddyRoute ok = %v, want %v", ok, tc.wantOk)
			}
			if tc.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrText) {
					t.Fatalf("CaddyRoute err = %v, want containing %q", err, tc.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("CaddyRoute err = %v", err)
			}
			_ = route
		})
	}
}

func TestGeneratedConfigPropagatesRenderError(t *testing.T) {
	// Empty domain makes renderer.RenderPanelCaddyfile fail while CaddyRoute succeeds.
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	_, ok, err := access.GeneratedConfig(generatedconfig.NewPaths(filepath.FromSlash("/etc/veil")))
	if ok {
		t.Fatal("Expected GeneratedConfig to return ok=false")
	}
	if err == nil || !strings.Contains(err.Error(), "domain is required") {
		t.Fatalf("Expected renderer domain error, got ok=%v err=%v", ok, err)
	}
}

func TestApplyIntentCaddyRequiresDomainAndEmail(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	intent := access.ApplyIntent(nil)
	if len(intent.Errors) != 1 || !strings.Contains(intent.Errors[0], "--domain and --email are required") {
		t.Fatalf("intent errors = %+v", intent.Errors)
	}
	if len(intent.Configs) != 0 || len(intent.Actions) != 0 || len(intent.Runtimes) != 0 {
		t.Fatalf("expected empty intent fields, got %+v", intent)
	}
}

func TestApplyIntentPropagatesCaddyRouteError(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:0", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	intent := access.ApplyIntent(nil)
	if len(intent.Errors) != 1 || !strings.Contains(intent.Errors[0], "panelListen must be host:port") {
		t.Fatalf("intent errors = %+v", intent.Errors)
	}
}

func TestApplyIntentSelectsInboundOn443(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	inbounds := []model.Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
	}
	intent := access.ApplyIntent(inbounds)
	if !contains(intent.Configs, "/etc/veil/generated/caddy/naive.Caddyfile") {
		t.Fatalf("expected naive.Caddyfile config, got %+v", intent.Configs)
	}
	if !contains(intent.Actions, "reload veil-caddy@naive.service") {
		t.Fatalf("expected naive service action, got %+v", intent.Actions)
	}
	if !contains(intent.Runtimes, "veil-caddy@naive.service") {
		t.Fatalf("expected naive runtime, got %+v", intent.Runtimes)
	}
	if len(intent.Errors) != 0 {
		t.Fatalf("expected no errors, got %+v", intent.Errors)
	}
}

func TestApplyIntentFallsBackToFirstCaddyProtocolInbound(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	inbounds := []model.Inbound{
		{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 8080, Enabled: true},
		{Name: "naive1", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true},
		{Name: "naive2", Protocol: "naiveproxy", Transport: "tcp", Port: 8444, Enabled: true},
	}
	intent := access.ApplyIntent(inbounds)
	if !contains(intent.Configs, "/etc/veil/generated/caddy/naive1.Caddyfile") {
		t.Fatalf("expected naive1.Caddyfile config, got %+v", intent.Configs)
	}
	if len(intent.Errors) != 0 {
		t.Fatalf("expected no errors, got %+v", intent.Errors)
	}
}

func TestApplyIntentSkipsDisabledInbounds(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	inbounds := []model.Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: false},
	}
	intent := access.ApplyIntent(inbounds)
	if !contains(intent.Configs, "/etc/veil/generated/caddy/panel.Caddyfile") {
		t.Fatalf("expected panel.Caddyfile fallback config, got %+v", intent.Configs)
	}
	if len(intent.Errors) != 0 {
		t.Fatalf("expected no errors, got %+v", intent.Errors)
	}
}

func TestApplyIntentPrefers443InboundOverFallback(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, func(protocol string) bool { return protocol == "naiveproxy" })
	inbounds := []model.Inbound{
		{Name: "naive-fallback", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true},
		{Name: "naive-443", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
	}
	intent := access.ApplyIntent(inbounds)
	if !contains(intent.Configs, "/etc/veil/generated/caddy/naive-443.Caddyfile") {
		t.Fatalf("expected 443 inbound config, got %+v", intent.Configs)
	}
}

func TestApplyIntentRequiresCaddyFuncNil(t *testing.T) {
	settings := model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", Domain: "panel.example.com", Email: "admin@example.com", WebBasePath: "panel-secret"}
	access := New(settings, nil)
	inbounds := []model.Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
	}
	intent := access.ApplyIntent(inbounds)
	// With nil requiresCaddy, no inbound requires caddy, so fallback panel config is used.
	if !contains(intent.Configs, "/etc/veil/generated/caddy/panel.Caddyfile") {
		t.Fatalf("expected panel.Caddyfile fallback config, got %+v", intent.Configs)
	}
}

func TestGeneratedConfigReturnsFalseForNonCaddyMode(t *testing.T) {
	access := New(model.Settings{PanelAccess: "direct"}, nil)
	artifact, ok, err := access.GeneratedConfig(generatedconfig.NewPaths(filepath.FromSlash("/etc/veil")))
	if err != nil || ok {
		t.Fatalf("GeneratedConfig ok=%v err=%v", ok, err)
	}
	if artifact.Path != "" || artifact.Body != "" {
		t.Fatalf("expected empty artifact, got %+v", artifact)
	}
}
