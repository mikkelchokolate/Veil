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

// The sing-box WARP SOCKS inbound is TCP-only: a Hysteria2/Mieru UDP listener
// on the same port number is a valid co-existence, not a conflict.
func TestValidationAllowsUDPInboundOnWarpSocksPort(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Warp:     model.WarpConfig{Enabled: true, SocksPort: 40000},
		Inbounds: []model.Inbound{
			{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: 40000, Enabled: true},
		},
	}
	fields := map[string]json.RawMessage{
		"settings": json.RawMessage(`{}`),
		"inbounds": json.RawMessage(`[]`),
		"warp":     json.RawMessage(`{}`),
	}
	errs := NewValidation().ValidateSnapshot(snapshot, fields)
	// No error may mention the shared port number or warp at all: restricting
	// the needle to the current "conflicts with warp" phrasing would green a
	// regression that rejects the co-existence under different wording (#915).
	for _, err := range errs {
		if strings.Contains(err, "40000") || strings.Contains(strings.ToLower(err), "warp") {
			t.Fatalf("valid WARP-TCP + Hy2-UDP co-existence was rejected: %v", errs)
		}
	}
}

// The TCP reservation must still hold: a TCP inbound on the WARP socks port
// is a real collision.
func TestValidationRejectsTCPInboundOnWarpSocksPort(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Warp:     model.WarpConfig{Enabled: true, SocksPort: 40000},
		Inbounds: []model.Inbound{
			{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 40000, Enabled: true},
		},
	}
	fields := map[string]json.RawMessage{
		"settings": json.RawMessage(`{}`),
		"inbounds": json.RawMessage(`[]`),
		"warp":     json.RawMessage(`{}`),
	}
	errs := NewValidation().ValidateSnapshot(snapshot, fields)
	// The error must name the conflicting bind, not merely mention warp:
	// otherwise a vague unrelated warp error would green a missing
	// TCP-collision check (#915).
	if !validationContains(errs, "conflicts with warp") || !validationContains(errs, "40000") {
		t.Fatalf("TCP inbound on the WARP socks port was accepted: %v", errs)
	}
}

// #1191: the panel's internal Hysteria2 auth callback binds
// 127.40.0.1:61001 on every start, so any TCP listener on that port
// collides regardless of whether a Hysteria2 inbound exists — unlike the
// per-inbound stats port, this reservation is unconditional.
func TestValidationRejectsHy2AuthPortForTCPInboundWithoutHysteria(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{
			{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: runtimeports.Hysteria2HTTPAuthPort, Enabled: true},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "hysteria2 auth callback") {
		t.Fatalf("TCP inbound on the Hysteria2 auth port was accepted: %v", errs)
	}
}

func TestValidationRejectsHy2AuthPortForPanelListen(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelListen = "0.0.0.0:" + itoa(runtimeports.Hysteria2HTTPAuthPort)
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "panellisten") || !validationContains(errs, "hysteria2 auth callback") {
		t.Fatalf("panelListen on the Hysteria2 auth port was accepted: %v", errs)
	}
}

func TestValidationRejectsHy2AuthPortForPanelCaddyPublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	settings.PanelPublicPort = runtimeports.Hysteria2HTTPAuthPort
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "panelpublicport") || !validationContains(errs, "hysteria2 auth callback") {
		t.Fatalf("Panel Caddy public port on the Hysteria2 auth port was accepted: %v", errs)
	}
}

func TestValidationRejectsHy2AuthPortForNaiveEffectivePublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, ProtocolFields: map[string]any{"publicPort": float64(runtimeports.Hysteria2HTTPAuthPort)}},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "hysteria2 auth callback") {
		t.Fatalf("Naive public-port/Hysteria2 auth collision was accepted: %v", errs)
	}
}

func TestValidationRejectsHy2AuthPortForWarpSocksPort(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Warp:     model.WarpConfig{Enabled: true, SocksPort: runtimeports.Hysteria2HTTPAuthPort},
		Inbounds: []model.Inbound{{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}},
	}
	fields := map[string]json.RawMessage{
		"settings": json.RawMessage(`{}`),
		"inbounds": json.RawMessage(`[]`),
		"warp":     json.RawMessage(`{}`),
	}
	errs := NewValidation().ValidateSnapshot(snapshot, fields)
	if !validationContains(errs, "socksport") || !validationContains(errs, "hysteria2 auth callback") {
		t.Fatalf("WARP socksPort on the Hysteria2 auth port was accepted: %v", errs)
	}
}

// The auth callback is TCP-only: a UDP inbound on the same numeric port
// does not collide with it.
func TestValidationAllowsUDPInboundOnHy2AuthPort(t *testing.T) {
	settings := runtimeBindSettings()
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{
			{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: runtimeports.Hysteria2HTTPAuthPort, Enabled: true},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	for _, err := range errs {
		if strings.Contains(err, "61001") {
			t.Fatalf("UDP inbound on the Hysteria2 auth port was rejected: %v", errs)
		}
	}
}

func TestValidationAllowsNaiveToSharePanelCaddyPublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	settings.PanelPublicPort = 443
	snapshot := model.ManagementSnapshot{
		Settings: settings,
		Inbounds: []model.Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, ProtocolFields: map[string]any{"publicPort": float64(443)}}},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	for _, err := range errs {
		if strings.Contains(strings.ToLower(err), "port 443") || strings.Contains(strings.ToLower(err), "tcp:443") {
			t.Fatalf("intentional Panel/Naive shared Caddy bind was rejected: %v", errs)
		}
	}
}
