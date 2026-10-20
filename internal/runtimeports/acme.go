package runtimeports

import (
	"net"
	"sort"
	"strconv"
)

const (
	// ACMEHTTP01ParkedPort is the loopback-only TCP port acme.sh's
	// --standalone listener parks on when the managed Caddy edge owns the
	// public HTTP-01 port (#1181): Caddy proxies /.well-known/acme-challenge/*
	// to it so the panel IP certificate renews without competing for :80.
	// A wildcard/public TCP listener on the same numeric port claims the
	// loopback address too, so every renewal would fail with "port already
	// in use" — desired-state validation therefore reserves it for every TCP
	// listen, like the Hysteria2 auth callback (#1285).
	ACMEHTTP01ParkedPort = 44080

	// ACMEHTTP01ParkedHost is the loopback address the parked acme.sh listener
	// binds via --local-address (#1207). It lives in the reserved
	// 127.42.0.0/16 band — deliberately NOT 127.0.0.1 — because no unit-level
	// egress filter (veil-warp, veil-hysteria2@, veil-mieru) pierces that
	// band, so a sandboxed proxy process can neither claim the listener's
	// address ahead of acme.sh nor dial it to steal the challenge path.
	ACMEHTTP01ParkedHost = "127.42.0.2"
)

func ACMEHTTP01ParkedAddress() string {
	return net.JoinHostPort(ACMEHTTP01ParkedHost, strconv.Itoa(ACMEHTTP01ParkedPort))
}

// reservedInternalTCPPorts names every loopback TCP port a Veil-owned
// service may bind, together with the service that owns it. The owner strings
// match the wording management-state validation uses in its rejections.
var reservedInternalTCPPorts = []struct {
	port  int
	owner string
}{
	{CaddyAdminPort, "the Caddy admin listener"},
	{ACMEHTTP01ParkedPort, "the IP-certificate HTTP-01 listener"},
	{Hysteria2TrafficStatsPort, "Hysteria2 traffic statistics"},
	{Hysteria2HTTPAuthPort, "the Hysteria2 auth callback"},
}

// ReservedInternalTCPPorts returns, in ascending order, every loopback TCP
// port a Veil-owned service may bind. A public/wildcard TCP listener on any
// of these numeric ports collides with the service regardless of start
// order, so callers that pick or validate public TCP ports must avoid them.
func ReservedInternalTCPPorts() []int {
	ports := make([]int, 0, len(reservedInternalTCPPorts))
	for _, reserved := range reservedInternalTCPPorts {
		ports = append(ports, reserved.port)
	}
	sort.Ints(ports)
	return ports
}

// IsReservedInternalTCPPort reports whether port is one of
// ReservedInternalTCPPorts.
func IsReservedInternalTCPPort(port int) bool {
	_, ok := ReservedInternalTCPPortOwner(port)
	return ok
}

// ReservedInternalTCPPortOwner returns a human-readable name of the Veil
// service that owns port when port is one of ReservedInternalTCPPorts.
func ReservedInternalTCPPortOwner(port int) (string, bool) {
	for _, reserved := range reservedInternalTCPPorts {
		if port == reserved.port {
			return reserved.owner, true
		}
	}
	return "", false
}
