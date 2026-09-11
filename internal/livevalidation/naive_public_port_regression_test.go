package livevalidation

import (
	"context"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type recordingPortProbe struct {
	unavailable map[string]bool
	probed      []string
}

func (p *recordingPortProbe) Available(_ context.Context, transport string, port int) (bool, error) {
	key := bindingKey(transport, port)
	p.probed = append(p.probed, key)
	if p.unavailable[key] {
		return false, nil
	}
	return true, nil
}

func naivePublicInbound(port, publicPort int) model.Inbound {
	return model.Inbound{
		Name:      "public",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      port,
		Enabled:   true,
		ProtocolFields: map[string]any{
			"domain":     "vpn.example.com",
			"email":      "admin@example.com",
			"publicPort": publicPort,
		},
	}
}

func TestValidatorUsesNaivePublicPortNotFlatPort(t *testing.T) {
	probe := &recordingPortProbe{unavailable: map[string]bool{"tcp:2096": false, "tcp:10001": false, "tcp:8443": true}}
	validator := testValidator()
	validator.Ports = probe

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{
			PanelListen:   "127.0.0.1:2096",
			NaiveUsername: "veil",
			NaivePassword: "secret",
			Domain:        "vpn.example.com",
			Email:         "admin@example.com",
		},
		Inbounds: []model.Inbound{naivePublicInbound(2096, 8443)},
	})
	if hasIssueCode(response, "reserved_panel_port") {
		t.Fatalf("unused flat panel port should not be reserved: %+v", response.Issues)
	}

	probe = &recordingPortProbe{unavailable: map[string]bool{"tcp:10001": false, "tcp:8443": true}}
	validator = testValidator()
	validator.Ports = probe
	response = validator.Validate(context.Background(), Request{
		Settings: model.Settings{
			NaiveUsername: "veil",
			NaivePassword: "secret",
			Domain:        "vpn.example.com",
			Email:         "admin@example.com",
		},
		Inbounds: []model.Inbound{naivePublicInbound(10001, 8443)},
	})
	probed8443 := false
	probedFlat := false
	for _, key := range probe.probed {
		if key == "tcp:8443" {
			probed8443 = true
		}
		if key == "tcp:10001" {
			probedFlat = true
		}
	}
	if !probed8443 {
		t.Fatalf("did not probe public port 8443: %v", probe.probed)
	}
	if probedFlat {
		t.Fatalf("probed unused flat port: %v", probe.probed)
	}
	if !hasIssueCode(response, "port_in_use") {
		t.Fatalf("expected port_in_use for public port 8443: %+v", response.Issues)
	}
}

func TestValidatorOwnedBindingUsesNaivePublicPort(t *testing.T) {
	current := naivePublicInbound(10001, 443)
	candidate := naivePublicInbound(10001, 8443)
	probe := &recordingPortProbe{unavailable: map[string]bool{"tcp:8443": false, "tcp:10001": false}}
	validator := testValidator()
	validator.Ports = probe

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{
			NaiveUsername: "veil",
			NaivePassword: "secret",
			Domain:        "vpn.example.com",
			Email:         "admin@example.com",
		},
		Inbounds:        []model.Inbound{candidate},
		CurrentInbounds: []model.Inbound{current},
	})
	probed8443 := false
	for _, key := range probe.probed {
		if key == "tcp:8443" {
			probed8443 = true
		}
	}
	if !probed8443 {
		t.Fatalf("changed public port should be probed, got %v issues=%+v", probe.probed, response.Issues)
	}
}
