package managementstate

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

// #1285: acme.sh parks its --standalone HTTP-01 listener on
// 127.42.0.2:44080 whenever the managed Caddy edge owns :80, and a public
// TCP listener on 44080 claims that address too — every IP-cert renewal then
// fails with "port 44080 is already in use". Like the Hysteria2 auth callback
// (#1191) the reservation is unconditional: the listener only appears at
// renewal time, long after validation accepted the colliding port.
const acmeHTTP01Needle = "ip-certificate http-01 listener"

func TestValidationRejectsACMEHTTP01PortForTCPInbound(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: runtimeBindSettings(),
		Inbounds: []model.Inbound{
			{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: runtimeports.ACMEHTTP01ParkedPort, Enabled: true},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "inbounds[0]") || !validationContains(errs, acmeHTTP01Needle) {
		t.Fatalf("TCP inbound on the parked HTTP-01 port was accepted: %v", errs)
	}
}

func TestValidationRejectsACMEHTTP01PortForPanelListen(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelListen = "0.0.0.0:" + itoa(runtimeports.ACMEHTTP01ParkedPort)
	snapshot := model.ManagementSnapshot{Settings: settings}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "panellisten") || !validationContains(errs, acmeHTTP01Needle) {
		t.Fatalf("panelListen on the parked HTTP-01 port was accepted: %v", errs)
	}
}

func TestValidationRejectsACMEHTTP01PortForPanelCaddyPublicPort(t *testing.T) {
	settings := runtimeBindSettings()
	settings.PanelAccess = "caddy"
	settings.PanelDomain = "panel.example.test"
	settings.PanelPublicPort = runtimeports.ACMEHTTP01ParkedPort
	snapshot := model.ManagementSnapshot{Settings: settings}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, "panelpublicport") || !validationContains(errs, acmeHTTP01Needle) {
		t.Fatalf("Panel Caddy public port on the parked HTTP-01 port was accepted: %v", errs)
	}
}

func TestValidationRejectsACMEHTTP01PortForNaiveEffectivePublicPort(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: runtimeBindSettings(),
		Inbounds: []model.Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, ProtocolFields: map[string]any{"publicPort": float64(runtimeports.ACMEHTTP01ParkedPort)}},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	if !validationContains(errs, acmeHTTP01Needle) {
		t.Fatalf("Naive public-port/HTTP-01 collision was accepted: %v", errs)
	}
}

func TestValidationRejectsACMEHTTP01PortForWarpSocksPort(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: runtimeBindSettings(),
		Warp:     model.WarpConfig{Enabled: true, SocksPort: runtimeports.ACMEHTTP01ParkedPort},
	}
	fields := map[string]json.RawMessage{
		"settings": json.RawMessage(`{}`),
		"inbounds": json.RawMessage(`[]`),
		"warp":     json.RawMessage(`{}`),
	}
	errs := NewValidation().ValidateSnapshot(snapshot, fields)
	if !validationContains(errs, "socksport") || !validationContains(errs, acmeHTTP01Needle) {
		t.Fatalf("WARP socksPort on the parked HTTP-01 port was accepted: %v", errs)
	}
}

// The parked listener is TCP-only: a UDP inbound on 44080 does not collide.
func TestValidationAllowsUDPInboundOnACMEHTTP01Port(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: runtimeBindSettings(),
		Inbounds: []model.Inbound{
			{Name: "hy", Protocol: "hysteria2", Transport: "udp", Port: runtimeports.ACMEHTTP01ParkedPort, Enabled: true},
		},
	}
	errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
	port := strconv.Itoa(runtimeports.ACMEHTTP01ParkedPort)
	for _, err := range errs {
		if strings.Contains(err, port) {
			t.Fatalf("UDP inbound on the parked HTTP-01 port was rejected: %v", errs)
		}
	}
}

// Control: neighbouring TCP ports stay valid, so the reservation is exact.
func TestValidationAllowsTCPInboundNextToACMEHTTP01Port(t *testing.T) {
	for _, port := range []int{runtimeports.ACMEHTTP01ParkedPort - 1, runtimeports.ACMEHTTP01ParkedPort + 1} {
		snapshot := model.ManagementSnapshot{
			Settings: runtimeBindSettings(),
			Inbounds: []model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: port, Enabled: true}},
		}
		errs := NewValidation().ValidateSnapshot(snapshot, runtimeBindValidationFields())
		if validationContains(errs, acmeHTTP01Needle) {
			t.Fatalf("TCP inbound on neighbouring port %d was rejected: %v", port, errs)
		}
	}
}
