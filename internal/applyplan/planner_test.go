package applyplan

import (
	"errors"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestPlannerBuildsManagementApplyIntentFromPanelProtocolsWarpAndRouting(t *testing.T) {
	plan := Build(Input{
		PanelAccess: Material{Configs: []string{"/etc/veil/generated/caddy/Caddyfile"}, Actions: []string{"reload veil-naive.service"}, Runtimes: []string{"veil-naive.service"}},
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
	for _, want := range []string{"/etc/veil/generated/caddy/Caddyfile", "/etc/veil/generated/mieru/server_config.json", "/etc/veil/generated/sing-box/warp.json", "/etc/veil/generated/rules/geoip.dat"} {
		if !contains(plan.Configs, want) {
			t.Fatalf("configs missing %q: %+v", want, plan.Configs)
		}
	}
	for _, want := range []string{"validate management state", "stage generated configs", "reload veil-naive.service", "restart veil-mieru.service", "reload veil-warp.service"} {
		if !contains(plan.Actions, want) {
			t.Fatalf("actions missing %q: %+v", want, plan.Actions)
		}
	}
	for _, want := range []string{"veil-naive.service", "veil-mieru.service", "veil-warp.service"} {
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
