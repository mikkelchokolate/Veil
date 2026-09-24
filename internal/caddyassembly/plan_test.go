package caddyassembly

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildRenderPlanPanelAndNaive(t *testing.T) {
	settings := model.Settings{
		PanelAccess:     "caddy",
		PanelDomain:     "panel.example.com",
		PanelPublicPort: 443,
		PanelEmail:      "admin@example.com",
	}
	inbounds := []model.Inbound{
		{
			Name:     "naive-1",
			Protocol: "naiveproxy",
			Enabled:  true,
			ProtocolFields: map[string]any{
				"domain":     "proxy.example.com",
				"transport":  "tcp",
				"publicPort": 8443,
			},
		},
	}
	// Use the final plan so the happy path also proves the issues slice stays
	// empty — a silently-raised ACME issue must not hide behind a passing
	// owner check (#845).
	plan, owners, issues, err := BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("happy-path plan raised issues: %+v", issues)
	}
	panelKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	if owners[panelKey].Kind != bindregistry.BindOwnerPanelCaddy {
		t.Error("expected Panel Caddy owner on TCP 443")
	}
	// Every Caddy bind owner carries the consolidated unit — a blanked or
	// renamed ServiceName detaches firewall/systemd consumers (#971).
	if owners[panelKey].ServiceName != "veil-caddy.service" {
		t.Errorf("panel owner ServiceName = %q, want veil-caddy.service", owners[panelKey].ServiceName)
	}
	if plan.Servers[panelKey].Kind != CaddyOwnerPanel {
		t.Error("expected Panel server in render plan")
	}
	naiveKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}
	if owners[naiveKey].Kind != bindregistry.BindOwnerNaive {
		t.Error("expected naive owner on TCP 8443")
	}
	if owners[naiveKey].ServiceName != "veil-caddy.service" {
		t.Errorf("naive owner ServiceName = %q, want veil-caddy.service", owners[naiveKey].ServiceName)
	}

	// ACME enrollment is part of the contract, not just bind ownership: both
	// domains must be enrolled with the panel email and the right owners —
	// dropping ResolveDomainCertSpecs enrollment stays green otherwise (#845).
	panelSpec, ok := plan.Domains["panel.example.com"]
	if !ok {
		t.Fatalf("panel domain missing from cert specs: %+v", plan.Domains)
	}
	if panelSpec.Email != "admin@example.com" || !panelSpec.Owners.Panel {
		t.Errorf("panel domain spec wrong: %+v", panelSpec)
	}
	naiveSpec, ok := plan.Domains["proxy.example.com"]
	if !ok {
		t.Fatalf("naive domain missing from cert specs: %+v", plan.Domains)
	}
	if naiveSpec.Email != "admin@example.com" {
		t.Errorf("naive domain email = %q, want admin@example.com", naiveSpec.Email)
	}
	if len(naiveSpec.Owners.NaiveInboundNames) != 1 || naiveSpec.Owners.NaiveInboundNames[0] != "naive-1" {
		t.Errorf("naive domain owners wrong: %+v", naiveSpec.Owners)
	}
}

func TestBuildRenderPlanRejectsNaiveNonTCPTransport(t *testing.T) {
	settings := model.Settings{}
	inbounds := []model.Inbound{
		{
			Name:     "naive-quic",
			Protocol: "naiveproxy",
			Enabled:  true,
			ProtocolFields: map[string]any{
				"domain":     "proxy.example.com",
				"transport":  "quic",
				"publicPort": 8443,
			},
		},
	}
	_, _, err := BuildRenderPlan(settings, inbounds, nil)
	if err == nil {
		t.Fatal("expected error for non-tcp naive transport")
	}
	if !strings.Contains(err.Error(), "only tcp is supported") {
		t.Fatalf("expected unsupported transport error, got %v", err)
	}
}
