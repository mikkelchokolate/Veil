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
	plan, owners, err := BuildRenderPlan(settings, inbounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	panelKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	if owners[panelKey].Kind != bindregistry.BindOwnerPanelCaddy {
		t.Error("expected Panel Caddy owner on TCP 443")
	}
	if plan.Servers[panelKey].Kind != CaddyOwnerPanel {
		t.Error("expected Panel server in render plan")
	}
	naiveKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}
	if owners[naiveKey].Kind != bindregistry.BindOwnerNaive {
		t.Error("expected naive owner on TCP 8443")
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
