package applyplan

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestPlannerBuildsStructuredOperationsDeterministically(t *testing.T) {
	plan := Build(Input{
		GeneratedRoot: "/var/lib/veil/staging",
		LiveRoot:      "/etc/veil/generated",
		PanelAccess: Material{
			Configs: []string{"/etc/veil/generated/caddy/edge.Caddyfile"},
			Actions: []string{"reload veil-caddy@edge.service"},
		},
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true,
		}},
		Capabilities: []ProtocolCapability{{
			Protocol: "mieru",
			Config:   "/etc/veil/generated/mieru/server_config.json",
			Action:   "restart veil-mieru.service",
		}},
		RuntimeUnits: []string{"veil-mieru.service"},
	})

	want := []model.ApplyOperation{
		{
			Type:              "promote_file",
			Source:            "/var/lib/veil/staging/caddy/edge.Caddyfile",
			Destination:       "/etc/veil/generated/caddy/edge.Caddyfile",
			InterruptionRisk:  "reload",
			RollbackAvailable: true,
			ValidationSource:  "render-and-live-host",
		},
		{
			Type:              "promote_file",
			Source:            "/var/lib/veil/staging/mieru/server_config.json",
			Destination:       "/etc/veil/generated/mieru/server_config.json",
			InterruptionRisk:  "reload",
			RollbackAvailable: true,
			ValidationSource:  "render-and-live-host",
		},
		{
			Type:              "reload_service",
			Unit:              "veil-caddy@edge.service",
			InterruptionRisk:  "reload",
			RollbackAvailable: true,
			ValidationSource:  "managed-unit-catalog",
		},
		{
			Type:              "restart_service",
			Unit:              "veil-mieru.service",
			InterruptionRisk:  "connection-drop",
			RollbackAvailable: true,
			ValidationSource:  "managed-unit-catalog",
		},
	}
	if !reflect.DeepEqual(plan.Operations, want) {
		t.Fatalf("operations:\n got: %#v\nwant: %#v", plan.Operations, want)
	}
}

func TestPlannerStructuredPreviewDoesNotContainSecrets(t *testing.T) {
	plan := Build(Input{
		GeneratedRoot: "/var/lib/veil/staging",
		LiveRoot:      "/etc/veil/generated",
		Settings: model.Settings{
			NaivePassword:     "naive-super-secret",
			Hysteria2Password: "hy2-super-secret",
		},
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "inbound-super-secret",
		}},
		Capabilities: []ProtocolCapability{{
			Protocol: "mieru",
			Config:   "/etc/veil/generated/mieru/server_config.json",
			Action:   "restart veil-mieru.service",
		}},
	})
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"naive-super-secret", "hy2-super-secret", "inbound-super-secret"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("structured preview leaked %q: %s", secret, body)
		}
	}
}

func TestPlannerBuildsManagementApplyIntentFromPanelProtocolsWarpAndRouting(t *testing.T) {
	plan := Build(Input{
		PanelAccess: Material{Configs: []string{"/etc/veil/generated/caddy/panel.Caddyfile"}, Actions: []string{"reload veil-caddy@panel.service"}, Runtimes: []string{"veil-caddy@panel.service"}},
		Settings:    model.Settings{Domain: "vpn.example.com"},
		Inbounds: []model.Inbound{
			{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "disabled", Protocol: "mieru", Transport: "tcp", Port: 444, Enabled: false},
		},
		Rules:         []model.RoutingRule{{Name: "warp-rule", Match: "geosite:test", Outbound: "warp", Enabled: true}},
		RoutingSource: model.RoutingSource{Files: []model.RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.com/geoip.dat"}}},
		Warp:          model.WarpConfig{Enabled: true},
		Capabilities: []ProtocolCapability{{
			Protocol: "mieru",
			Config:   "/etc/veil/generated/mieru/server_config.json",
			Action:   "restart veil-mieru.service",
		}},
		RuntimeUnits: []string{"veil-mieru.service", "veil-warp.service"},
		WarpAction:   "reload veil-warp.service",
	})
	if !plan.Valid {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
	for _, want := range []string{"/etc/veil/generated/caddy/panel.Caddyfile", "/etc/veil/generated/mieru/server_config.json", "/etc/veil/generated/sing-box/warp.json", "/etc/veil/generated/rules/geoip.dat"} {
		if !contains(plan.Configs, want) {
			t.Fatalf("configs missing %q: %+v", want, plan.Configs)
		}
	}
	for _, want := range []string{"validate management state", "stage generated configs", "reload veil-caddy@panel.service", "restart veil-mieru.service", "reload veil-warp.service"} {
		if !contains(plan.Actions, want) {
			t.Fatalf("actions missing %q: %+v", want, plan.Actions)
		}
	}
	for _, want := range []string{"veil-caddy@panel.service", "veil-mieru.service", "veil-warp.service"} {
		if !contains(plan.Runtimes, want) {
			t.Fatalf("runtimes missing %q: %+v", want, plan.Runtimes)
		}
	}
}

func TestPlannerRejectsInvalidEnabledInboundAndCardinality(t *testing.T) {
	plan := Build(Input{
		Inbounds: []model.Inbound{
			{Name: "a", Protocol: "unknown", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "b", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
		},
		Capabilities: []ProtocolCapability{{Protocol: "mieru", Config: "/config", Action: "restart unit"}},
		ValidateCardinality: func(model.Settings, []model.Inbound) error {
			return errors.New("too many generated configs")
		},
	})
	if plan.Valid {
		t.Fatalf("expected invalid plan: %+v", plan)
	}
	joined := strings.Join(plan.Errors, "\n")
	for _, want := range []string{"unsupported inbound protocol: unknown", "duplicate enabled inbound transport/port", "too many generated configs"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors missing %q: %+v", want, plan.Errors)
		}
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
