package renderer

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// egressDenyCIDRs is the canonical destination deny set applied by every
// egress guard Veil renders (Hysteria2 ACL, sing-box route rules, Caddy
// forward_proxy ACL, Mieru egress rules, and the systemd IP egress filter).
// Proxy client sessions must never reach server-local, private, link-local,
// CGNAT, multicast, reserved or unspecified destinations: a proxy that can
// reach them turns into a local pivot (e.g. Caddy's unauthenticated admin
// API on 127.0.0.1:2019) (issues #1095, #1096, #1097).
//
// The list is deliberately exhaustive rather than "private only": a proxy
// egress policy must fail closed on every non-public destination class.
var egressDenyCIDRs = []string{
	// IPv4 "this network" / unspecified (0.0.0.0 is also the unspecified
	// address a misconfigured client may dial).
	"0.0.0.0/8",
	// RFC 1918 private IPv4.
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	// RFC 6598 CGNAT shared address space.
	"100.64.0.0/10",
	// Loopback — host control planes live here (Caddy admin 127.0.0.1:2019,
	// panel backend, mita appctl over TCP, DNS stub resolvers).
	"127.0.0.0/8",
	// Link-local — includes cloud metadata 169.254.169.254 (SSRF credential
	// theft) and any neighbour-discovery service.
	"169.254.0.0/16",
	// IPv4 multicast and the reserved former class-E block.
	"224.0.0.0/4",
	"240.0.0.0/4",
	// IPv6 unspecified.
	"::/128",
	// IPv6 loopback.
	"::1/128",
	// IPv4-mapped IPv6 — proxies that resolve/connect through this form must
	// not smuggle IPv4 loopback/private destinations past the IPv4 rules.
	// These are the mapped forms of the IPv4 denies above, NOT the blanket
	// mapped prefix: hysteria's ACL engine normalizes a plain IPv4 destination
	// into ::ffff: space before matching IPv6 prefixes, so reject(::ffff:0:0/96)
	// would deny every IPv4 egress — verified empirically against the pinned
	// hysteria release (#1097 follow-up).
	"::ffff:0.0.0.0/104",     // mapped 0.0.0.0/8
	"::ffff:10.0.0.0/104",    // mapped 10.0.0.0/8
	"::ffff:172.16.0.0/108",  // mapped 172.16.0.0/12
	"::ffff:192.168.0.0/112", // mapped 192.168.0.0/16
	"::ffff:100.64.0.0/106",  // mapped 100.64.0.0/10
	"::ffff:127.0.0.0/104",   // mapped 127.0.0.0/8
	"::ffff:169.254.0.0/112", // mapped 169.254.0.0/16
	"::ffff:224.0.0.0/100",   // mapped 224.0.0.0/4
	"::ffff:240.0.0.0/100",   // mapped 240.0.0.0/4
	// RFC 6052 NAT64 well-known prefix — embeds arbitrary IPv4 destinations.
	"64:ff9b::/96",
	// RFC 4193 IPv6 unique-local.
	"fc00::/7",
	// IPv6 link-local.
	"fe80::/10",
	// IPv6 multicast.
	"ff00::/8",
}

// EgressDenyCIDRs returns the canonical egress deny list. The result is a
// fresh slice so callers may embed it in rendered documents without aliasing.
func EgressDenyCIDRs() []string {
	return append([]string(nil), egressDenyCIDRs...)
}

// egressDenySystemd renders the deny list as one IPAddressDeny= line.
func egressDenySystemd() string {
	return strings.Join(egressDenyCIDRs, " ")
}

// Loopback exceptions for units under the egress IP filter. systemd evaluates
// IPAddressAllow= before IPAddressDeny=, so these /16s pierce the 127.0.0.0/8
// deny without reopening other loopback services:
//
//   - 127.0.0.53 / 127.0.0.54: systemd-resolved stub resolvers, so DNS keeps
//     working on hosts whose resolv.conf points at the stub.
//   - 127.40.0.0/16: the reserved loopback band used for the per-inbound
//     Hysteria2 traffic-stats listeners (runtimeports.Hysteria2TrafficStatsHost).
//   - 127.41.0.0/16: the reserved loopback band used for the WARP sing-box
//     SOCKS5 listener (default 127.41.0.1:40000). Keeping the unauthenticated
//     SOCKS bridge on its own band lets the filter let protocol daemons reach
//     it without also permitting 127.0.0.1 (Caddy admin, panel backend).
const (
	egressAllowResolvedStub = "127.0.0.53/32 127.0.0.54/32"
	egressAllowStatsBand    = "127.40.0.0/16"
	// Single-sourced from the model so the ACL pierce and the socksListen
	// validation contract can never drift apart (#1160).
	egressAllowWarpSocksBand = model.WarpSocksEgressBand
	egressAllowHysteria2Unit = egressAllowResolvedStub + " " + egressAllowStatsBand + " " + egressAllowWarpSocksBand
	egressAllowOlcrtcUnit    = egressAllowResolvedStub + " " + egressAllowWarpSocksBand
	egressAllowMieruUnit     = egressAllowResolvedStub
	// egressAllowWarpUnit additionally pierces 127.0.0.1 for the veil-warp
	// unit: a client of its local SOCKS listener always shows 127.0.0.1 as the
	// connection source (the kernel does not pick a 127.41/16 source for a
	// 127.41/16 destination), so the listener-side ingress check can only be
	// satisfied by allowing exactly that host. The hole this opens — the warp
	// daemon dialing 127.0.0.1 itself — stays closed at the sing-box route
	// layer, whose leading block rule rejects every proxied restricted
	// destination before any dial (#1096).
	egressAllowWarpUnit = "127.0.0.1/32 " + egressAllowResolvedStub
)
