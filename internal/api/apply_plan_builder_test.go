package api

import (
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestBuildApplyPlanIncludesCaddyJSONArtifact(t *testing.T) {
	settings := Settings{
		PanelListen: "0.0.0.0:8080",
		Mode:        "server",
		PanelAccess: "caddy",
		PanelDomain: "panel.example.com",
		PanelEmail:  "admin@example.com",
	}
	inbounds := []Inbound{}
	plan := BuildApplyPlan(ApplyPlanInput{Settings: settings, Inbounds: inbounds})
	if !plan.Valid {
		t.Fatalf("plan invalid: %v", plan.Errors)
	}
	found := false
	for _, c := range plan.Configs {
		if c == "/etc/veil/generated/caddy/config.json" {
			found = true
		}
	}
	if !found {
		t.Error("expected Caddy JSON config artifact in plan")
	}
}

func TestBuildApplyPlanUsesApplyRootForStructuredOperations(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		ApplyRoot: "/srv/veil",
		Settings:  Settings{PanelListen: "127.0.0.1:2096", Mode: "server"},
		Inbounds: []Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret",
		}},
	})

	want := []model.ApplyOperation{
		{
			Type:              "promote_file",
			Source:            "/srv/veil/generated/mieru/server_config.json",
			Destination:       "/srv/veil/live/mieru/server_config.json",
			InterruptionRisk:  "reload",
			RollbackAvailable: true,
			ValidationSource:  "render-and-live-host",
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

func TestBuildApplyPlanRejectsRoutingRuleUsingDisabledWarp(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{},
		Rules:    []RoutingRule{{Name: "non-ru", Match: "geosite:geolocation-!ru", Outbound: "warp", Enabled: true}},
	})
	if plan.Valid {
		t.Fatalf("plan should be invalid: %+v", plan)
	}
	if !containsApplyPlanString(plan.Errors, "routing rule non-ru requires WARP to be enabled") {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
}

func TestBuildApplyPlanUsesRenderValidationCallbacks(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings:                Settings{},
		RenderSettingsAvailable: true,
		Inbounds:                []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
		ValidateInboundRender: func(inbound Inbound) error {
			return errApplyPlanTest("render failed")
		},
	})
	if plan.Valid {
		t.Fatalf("plan should be invalid: %+v", plan)
	}
	if !containsApplyPlanString(plan.Errors, "render failed") {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
}

func TestBuildApplyPlanUsesMieruRenderValidationWithoutRenderSettings(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{},
		Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
		ValidateInboundRender: func(inbound Inbound) error {
			if inbound.Protocol == "mieru" {
				return errApplyPlanTest("mieru render failed")
			}
			return nil
		},
	})
	if plan.Valid {
		t.Fatalf("plan should be invalid: %+v", plan)
	}
	if !containsApplyPlanString(plan.Errors, "mieru render failed") {
		t.Fatalf("plan errors = %+v", plan.Errors)
	}
}

type errApplyPlanTest string

func (e errApplyPlanTest) Error() string { return string(e) }

func containsApplyPlanString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAddInboundBindOwnersRespectsTransportAndRuntimeUnit(t *testing.T) {
	catalog := service.NewManagedRuntimeCatalog([]service.ManagedRuntime{
		{Name: "mieru", Protocol: "mieru", Unit: "veil-mieru.service"},
		{Name: "olcrtc", Protocol: "olcrtc", Unit: "veil-olcrtc@.service", TemplateUnit: "veil-olcrtc@.service"},
		{Name: "other", Protocol: "other", Unit: "veil-other.service"},
	})
	inbounds := []Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 444, Enabled: true},
		{Name: "rtc-1", Protocol: "olcrtc", Transport: "udp", Port: 445, Enabled: true},
		{Name: "other-tcp", Protocol: "other", Transport: "tcp", Port: 446, Enabled: true},
	}
	owners := make(map[bindregistry.BindKey]bindregistry.BindOwner)
	conflicts := addInboundBindOwners(inbounds, owners, catalog)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}

	cases := []struct {
		port    int
		network bindregistry.ListenNetwork
		unit    string
	}{
		{443, bindregistry.ListenTCP, "veil-mieru.service"},
		{444, bindregistry.ListenUDP, "veil-mieru.service"},
		{445, bindregistry.ListenUDP, "veil-olcrtc@rtc-1.service"},
		{446, bindregistry.ListenTCP, "veil-other.service"},
	}
	for _, tc := range cases {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: tc.port, Network: tc.network}
		owner, ok := owners[key]
		if !ok {
			t.Fatalf("missing owner for %s:%d", tc.network, tc.port)
		}
		if owner.ServiceName != tc.unit {
			t.Errorf("port %d %s unit = %q, want %q", tc.port, tc.network, owner.ServiceName, tc.unit)
		}
		if owner.Kind != bindregistry.BindOwnerInbound {
			t.Errorf("port %d %s kind = %q, want %q", tc.port, tc.network, owner.Kind, bindregistry.BindOwnerInbound)
		}
	}
}
