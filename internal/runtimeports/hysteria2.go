package runtimeports

import (
	"fmt"
	"net"
	"strconv"
)

// Hysteria2TrafficStatsPort is reserved for the local-only Hysteria2 Traffic
// Stats API. Each enabled Hysteria2 inbound binds this port on a distinct
// 127/8 address derived from its unique public UDP port, so multiple Hysteria2
// processes can expose accounting without colliding with each other.
const Hysteria2TrafficStatsPort = 61000

// Hysteria2TrafficStatsHost maps a validated public UDP port to a stable,
// distinct loopback address in the reserved 127.40.0.0/16 band. Linux treats
// the entire 127/8 prefix as loopback, and the dedicated band lets the
// unit-level egress filter (IPAddressAllow/IPAddressDeny on
// veil-hysteria2@.service) pierce just this listener while the rest of 127/8
// stays denied to proxy sessions (#1095/#1097). Public Hysteria2 ports are
// unique in desired-state validation, making this mapping collision-free
// across enabled Hysteria2 inbounds.
func Hysteria2TrafficStatsHost(publicPort int) string {
	if publicPort < 1 || publicPort > 65535 {
		return "127.0.0.1"
	}
	return fmt.Sprintf("127.40.%d.%d", (publicPort>>8)&0xff, publicPort&0xff)
}

func Hysteria2TrafficStatsAddress(publicPort int) string {
	return net.JoinHostPort(Hysteria2TrafficStatsHost(publicPort), strconv.Itoa(Hysteria2TrafficStatsPort))
}

func Hysteria2TrafficStatsEndpoint(publicPort int) string {
	return "http://" + Hysteria2TrafficStatsAddress(publicPort) + "/traffic"
}

// Hysteria2HTTPAuthPort is reserved for the panel-side internal Hysteria2
// HTTP authentication callback (#1173). Unlike the stats API it is a single
// listener shared by every Hysteria2 inbound: the per-inbound identity is
// carried in the request path, so it binds one fixed address inside the
// 127.40.0.0/16 band the unit egress filter already pierces. The distinct
// port means it can never collide with a per-inbound stats listener even
// when the mapped stats host equals Hysteria2HTTPAuthHost.
const Hysteria2HTTPAuthPort = 61001

// Hysteria2HTTPAuthHost is the fixed loopback address the internal auth
// listener binds. It must live inside 127.40.0.0/16 — the only non-resolver
// loopback band veil-hysteria2@.service units are allowed to dial — while
// staying outside the reserved WARP band (127.41.0.0/16).
const Hysteria2HTTPAuthHost = "127.40.0.1"

func Hysteria2HTTPAuthAddress() string {
	return net.JoinHostPort(Hysteria2HTTPAuthHost, strconv.Itoa(Hysteria2HTTPAuthPort))
}
