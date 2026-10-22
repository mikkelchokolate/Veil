package renderer

import (
	"net/netip"
	"strings"
)

// Mieru (mita v3.36.1) has no upstream resolver setting: ServerConfig.dns
// only carries dualStack, so it always resolves through /etc/resolv.conf.
// Under the protocol egress filter that fails on every host whose
// nameservers are all in a denied range (#1288). Hysteria2 and WARP get a
// pinned DoH server in their rendered config; Mieru gets it at the unit
// level instead. At every start, veil-mieru runs `veil egress-resolv-conf`
// as root (ExecStartPre=+), which writes MieruResolvConfPath only on such a
// host, and the unit bind-mounts that file read-only over /etc/resolv.conf
// inside its own mount namespace (BindReadOnlyPaths=-, ignored when the
// file is absent). Hosts with a reachable nameserver get no file and keep
// the host resolver unchanged.
//
// The fallback is plain DNS to Cloudflare's public resolvers on port 53:
// mita's resolver is the Go/libc stub, which cannot speak DoH. Public
// addresses are outside the deny set, so the filter lets them through
// without opening any private range. The family follows the DoH pin chosen
// for Hysteria2 and WARP (IPv6 only on hosts that route IPv6 and not IPv4
// to Cloudflare).
//
// A missing /etc/resolv.conf is left alone: bind-mounting over a path that
// does not exist would make systemd create it on the host's /etc.
const MieruResolvConfPath = "/run/veil-mieru/resolv.conf"

var (
	mieruFallbackNameserversV4 = []string{"1.1.1.1", "1.0.0.1"}
	mieruFallbackNameserversV6 = []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}
)

// mieruFallbackNameservers returns the public nameservers matching the
// family of the pinned DoH address.
func mieruFallbackNameservers(pinnedDoH string) []string {
	if addr, err := netip.ParseAddrPort(pinnedDoH); err == nil && addr.Addr().Is6() {
		return append([]string(nil), mieruFallbackNameserversV6...)
	}
	return append([]string(nil), mieruFallbackNameserversV4...)
}

// MieruResolvConf returns the resolv.conf veil-mieru must see, or ok=false
// when it should keep the host's own file.
func MieruResolvConf(report EgressDNSReport) (body []byte, ok bool) {
	if report.ReadError != "" || report.Reachable || report.ResolvConfMissing || len(report.MieruNameservers) == 0 {
		return nil, false
	}
	var b strings.Builder
	b.WriteString("# Written by veil at veil-mieru start (#1288): no /etc/resolv.conf\n")
	b.WriteString("# nameserver is reachable under the protocol egress filter.\n")
	for _, ns := range report.MieruNameservers {
		b.WriteString("nameserver " + ns + "\n")
	}
	return []byte(b.String()), true
}
