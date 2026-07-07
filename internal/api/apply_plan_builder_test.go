package api

import (
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

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

func TestBuildApplyPlanUsesPerInboundRuntimeActions(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		ApplyRoot: "/srv/veil",
		Settings:  Settings{PanelListen: "127.0.0.1:2096", Mode: "server"},
		Inbounds:  []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Password: "secret"}},
	})

	if containsApplyPlanString(plan.Actions, "restart veil-hysteria2@.service") {
		t.Fatalf("plan actions should not target template unit: %+v", plan.Actions)
	}
	if !containsApplyPlanString(plan.Actions, "restart veil-hysteria2@edge.service") {
		t.Fatalf("missing per-inbound restart action: %+v", plan.Actions)
	}
	want := model.ApplyOperation{Type: "restart_service", Unit: "veil-hysteria2@edge.service", InterruptionRisk: "connection-drop", RollbackAvailable: true, ValidationSource: "managed-unit-catalog"}
	if !containsApplyOperation(plan.Operations, want) {
		t.Fatalf("missing per-inbound restart operation in %+v", plan.Operations)
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

func containsApplyOperation(values []model.ApplyOperation, want model.ApplyOperation) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, want) {
			return true
		}
	}
	return false
}
