package livevalidation

import (
	"context"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

// TestValidatorAllowsProtocolChangeOnOwnedBinding covers #1030: an inbound
// update that changes the protocol while keeping name, transport and port is
// still the same Veil inbound — the old unit's listener must not report the
// candidate's own port as in-use, because the gated apply stops the old unit
// before the new listener binds.
func TestValidatorAllowsProtocolChangeOnOwnedBinding(t *testing.T) {
	current := model.Inbound{Name: "edge", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "old-secret"}
	candidate := model.Inbound{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "new-secret"}

	validator := testValidator()
	// The old-protocol unit still owns the socket.
	validator.Ports = fakePortProbe{available: map[string]bool{"tcp:443": false}}

	response := validator.Validate(context.Background(), Request{
		Inbounds:        []model.Inbound{candidate},
		CurrentInbounds: []model.Inbound{current},
	})

	if hasIssueCode(response, "port_in_use") {
		t.Fatalf("self-owned port reported busy on protocol change: %+v", response)
	}
}

// TestValidatorStillRejectsForeignPortOwner guards the negative side: a
// same-port inbound with a different name is not self-owned.
func TestValidatorStillRejectsForeignPortOwner(t *testing.T) {
	validator := testValidator()
	validator.Ports = fakePortProbe{available: map[string]bool{"tcp:443": false}}

	response := validator.Validate(context.Background(), Request{
		Inbounds:        []model.Inbound{{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "new-secret"}},
		CurrentInbounds: []model.Inbound{{Name: "other", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "old"}},
	})

	assertErrorIssue(t, response, "port_in_use")
}

// TestValidatorRejectsInternalReservedTCPPorts covers #1061: the loopback
// TCP ports Veil services bind (Hysteria2 traffic stats on 61000, managed
// Caddy admin on 2019) must be rejected for public TCP inbounds regardless
// of whether the service currently exists — a wildcard listener on either
// port claims the loopback address and wedges the service on first use.
func TestValidatorRejectsInternalReservedTCPPorts(t *testing.T) {
	for _, port := range []int{runtimeports.Hysteria2TrafficStatsPort, runtimeports.CaddyAdminPort} {
		response := testValidator().Validate(context.Background(), Request{
			Inbounds: []model.Inbound{{
				Name: "edge", Protocol: "mieru", Transport: "tcp", Port: port, Enabled: true, Password: "secret",
			}},
		})
		assertErrorIssue(t, response, "reserved_internal_port")
	}
}

// TestValidatorAllowsUDPOnReservedTCPPorts documents the transport split:
// the reserved loopback listeners are TCP-only, so a UDP inbound on the same
// numeric port does not collide with them.
func TestValidatorAllowsUDPOnReservedTCPPorts(t *testing.T) {
	for _, port := range []int{runtimeports.Hysteria2TrafficStatsPort, runtimeports.CaddyAdminPort} {
		response := testValidator().Validate(context.Background(), Request{
			Inbounds: []model.Inbound{{
				Name: "edge", Protocol: "mieru", Transport: "udp", Port: port, Enabled: true, Password: "secret",
			}},
		})
		if hasIssueCode(response, "reserved_internal_port") {
			t.Fatalf("UDP %d must not collide with a TCP-only reserved port: %+v", port, response)
		}
	}
}
