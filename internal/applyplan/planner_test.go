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
			Configs: []string{"/etc/veil/generated/caddy/config.json"},
			Actions: []string{"reload veil-caddy.service"},
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
			Source:            "/var/lib/veil/staging/caddy/config.json",
			Destination:       "/etc/veil/generated/caddy/config.json",
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
			Unit:              "veil-caddy.service",
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
		PanelAccess: Material{Configs: []string{"/etc/veil/generated/caddy/config.json"}, Actions: []string{"reload veil-caddy.service"}, Runtimes: []string{"veil-caddy.service"}},
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
		WarpAction:   "restart veil-warp.service",
	})
	if !plan.Valid {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
	for _, want := range []string{"/etc/veil/generated/caddy/config.json", "/etc/veil/generated/mieru/server_config.json", "/etc/veil/generated/sing-box/warp.json", "/etc/veil/generated/rules/geoip.dat"} {
		if !contains(plan.Configs, want) {
			t.Fatalf("configs missing %q: %+v", want, plan.Configs)
		}
	}
	for _, want := range []string{"validate management state", "stage generated configs", "reload veil-caddy.service", "restart veil-mieru.service", "restart veil-warp.service"} {
		if !contains(plan.Actions, want) {
			t.Fatalf("actions missing %q: %+v", want, plan.Actions)
		}
	}
	for _, want := range []string{"veil-caddy.service", "veil-mieru.service", "veil-warp.service"} {
		if !contains(plan.Runtimes, want) {
			t.Fatalf("runtimes missing %q: %+v", want, plan.Runtimes)
		}
	}
	// Every staged config must produce exactly one promote_file operation
	// whose destination is that config; the action verbs produce the matching
	// service operations.
	promoteDestinations := map[string]bool{}
	promoteCount := 0
	serviceOps := map[string]string{}
	for _, op := range plan.Operations {
		switch op.Type {
		case "promote_file":
			promoteCount++
			promoteDestinations[op.Destination] = true
			if op.Source == "" || op.Destination == "" {
				t.Fatalf("promote_file operation missing source/destination: %+v", op)
			}
		case "reload_service", "restart_service":
			serviceOps[op.Unit] = op.Type
		default:
			t.Fatalf("unexpected operation type %q: %+v", op.Type, op)
		}
	}
	if promoteCount != len(plan.Configs) {
		t.Fatalf("promote_file count=%d want=%d (one per config): %+v", promoteCount, len(plan.Configs), plan.Operations)
	}
	for _, config := range plan.Configs {
		if !promoteDestinations[config] {
			t.Fatalf("no promote_file operation targets config %q: %+v", config, plan.Operations)
		}
	}
	for unit, want := range map[string]string{
		"veil-caddy.service": "reload_service",
		"veil-mieru.service": "restart_service",
		"veil-warp.service":  "restart_service",
	} {
		if serviceOps[unit] != want {
			t.Fatalf("service op for %s=%q, want %q: %+v", unit, serviceOps[unit], want, plan.Operations)
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

// The preview must anchor displayed configs and promote destinations at the
// configured live root — a custom install previews the same tree the apply
// job promotes into, not /etc/veil/generated (issue #636).
func TestPlannerUsesCustomLiveRootForConfigsAndDestinations(t *testing.T) {
	plan := Build(Input{
		GeneratedRoot: "/custom/var/staging/generated",
		LiveRoot:      "/custom/etc/generated",
		Warp:          model.WarpConfig{Enabled: true},
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true,
		}},
		Capabilities: []ProtocolCapability{{
			Protocol: "mieru",
			Config:   "/custom/etc/generated/mieru/server_config.json",
			Action:   "restart veil-mieru.service",
		}},
		RoutingSource: model.RoutingSource{Files: []model.RoutingSourceFile{{Name: "geo.dat", URL: "https://example.com/geo.dat"}}},
	})
	for _, want := range []string{
		"/custom/etc/generated/mieru/server_config.json",
		"/custom/etc/generated/sing-box/warp.json",
		"/custom/etc/generated/rules/geo.dat",
	} {
		found := false
		for _, config := range plan.Configs {
			if config == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("plan.Configs missing %q: %v", want, plan.Configs)
		}
	}
	for _, op := range plan.Operations {
		if op.Type != "promote_file" {
			continue
		}
		if !strings.HasPrefix(op.Destination, "/custom/etc/generated/") {
			t.Fatalf("promote destination %q is not under the custom live root", op.Destination)
		}
		if !strings.HasPrefix(op.Source, "/custom/var/staging/generated/") {
			t.Fatalf("promote source %q is not under the staged generated root", op.Source)
		}
		// The staged subpath must be preserved end-to-end — a basename
		// fallback would collide with the live tree layout.
		rel := strings.TrimPrefix(op.Destination, "/custom/etc/generated/")
		if !strings.HasSuffix(op.Source, "/"+rel) {
			t.Fatalf("source %q does not preserve subpath %q", op.Source, rel)
		}
	}
}
