package managementstate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

func runtimeBindValidationFields() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"settings": json.RawMessage(`{}`),
		"inbounds": json.RawMessage(`[]`),
	}
}

func runtimeBindSettings() model.Settings {
	return model.Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "dev",
	}
}

func validationContains(errors []string, needle string) bool {
	needle = strings.ToLower(needle)
	for _, current := range errors {
		if strings.Contains(strings.ToLower(current), needle) {
			return true
		}
	}
	return false
}

func TestValidationRejectsCaddyAdminPortForMieru(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "mieru-admin", Protocol: "mieru", Transport: "tcp", Port: runtimeports.CaddyAdminPort, Enabled: true}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "caddy admin listener") {
		t.Fatalf("Caddy admin collision was accepted: %v", errs)
	}
}

func TestValidationUsesNaiveEffectivePublicPortForCrossProtocolCollision(t *testing.T) {
	settings := runtimeBindSettings()
	const publicPort = 24444
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, ProtocolFields: map[string]any{"publicPort": float64(publicPort)}},
			{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: publicPort, Enabled: true},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "duplicate transport/port tcp:24444") {
		t.Fatalf("Naive effective public-port collision was accepted: %v", errs)
	}
}

func TestValidationRejectsHysteriaStatsPortForNaiveEffectivePublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{
			{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, ProtocolFields: map[string]any{"publicPort": float64(runtimeports.Hysteria2TrafficStatsPort)}},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "reserved for hysteria2 traffic statistics") {
		t.Fatalf("Naive public-port/Hysteria stats collision was accepted: %v", errs)
	}
}

func TestValidationRejectsHysteriaStatsPortForPanelCaddyPublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	settings.PanelPublicPort = runtimeports.Hysteria2TrafficStatsPort
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "panelpublicport") || !validationContains(errs, "hysteria2 traffic statistics") {
		t.Fatalf("Panel Caddy/Hysteria stats collision was accepted: %v", errs)
	}
}

func TestValidationRejectsNonCaddyRuntimeOnPanelCaddyPublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	settings.PanelPublicPort = 443
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "caddy public panel listener") {
		t.Fatalf("Mieru/Panel Caddy public-port collision was accepted: %v", errs)
	}
}

func TestValidationAllowsNaiveToSharePanelCaddyPublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	settings.PanelPublicPort = 443
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, ProtocolFields: map[string]any{"publicPort": float64(443)}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	for _, err := range errs {
		if strings.Contains(strings.ToLower(err), "port 443") || strings.Contains(strings.ToLower(err), "tcp:443") {
			t.Fatalf("intentional Panel/Naive shared Caddy bind was rejected: %v", errs)
		}
	}
}
