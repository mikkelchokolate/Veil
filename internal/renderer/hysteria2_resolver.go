package renderer

import (
	"bufio"
	"bytes"
	"net/netip"
	"os"
	"strings"
)

// Hysteria2 runs under the kernel egress filter (IPAddressDeny= the full
// private/loopback/link-local/CGNAT set, see egress.go), which pierces only
// the systemd-resolved stubs. With no resolver: block, Hysteria2 uses
// /etc/resolv.conf (Veil always renders an ACL, so hysteria puts its system
// resolver in front of it). On a host whose nameservers are all in a denied
// range (AWS VPC 172.31.0.2 / 169.254.169.253, GCP/OCI 169.254.169.254,
// Alibaba 100.100.2.136, LXC/home-lab 10.x/192.168.x, local dnsmasq on
// 127.0.0.1), the cgroup BPF filter drops every lookup. Domain traffic and
// domain ACL rules then fail silently while the unit stays active (#1288).
//
// Only those hosts get a pinned public DNS-over-HTTPS resolver. A host
// whose resolv.conf already names a reachable nameserver (the resolved stub
// or a public IP) keeps it, along with its caching, its split-DNS, and its
// address family. The DoH resolver uses a literal IP, so it needs no
// bootstrap lookup. It listens on 443, so censorship-grade networks are not
// forced onto plaintext :53. It is IPv6 on hosts with an IPv6 default route
// and no IPv4 one, because hysteria accepts a single resolver address.
// Hysteria wraps the ACL engine with the resolver, so the reject() rules
// still match resolved addresses. The SOCKS5 (WARP) outbound ignores the
// resolve result and forwards the domain, so WARP-routed names are still
// resolved by sing-box.
const (
	hysteria2DoHResolverAddrV4  = "1.1.1.1:443"
	hysteria2DoHResolverAddrV6  = "[2606:4700:4700::1111]:443"
	hysteria2DoHResolverSNI     = "cloudflare-dns.com"
	hysteria2DoHResolverTimeout = "10s"
)

// Host probes. Tests replace them; production reads the live host.
var (
	hysteria2ReadResolvConf = func() ([]byte, error) { return os.ReadFile("/etc/resolv.conf") }
	hysteria2ReadIPv4Routes = func() ([]byte, error) { return os.ReadFile("/proc/net/route") }
	hysteria2ReadIPv6Routes = func() ([]byte, error) { return os.ReadFile("/proc/net/ipv6_route") }
	hysteria2ResolverProbe  = probeHysteria2Resolver
)

func probeHysteria2Resolver() *hysteria2ResolverYAML {
	conf, err := hysteria2ReadResolvConf()
	if err != nil && !os.IsNotExist(err) {
		// Unknown host state: keep the daemon's default behaviour rather
		// than guess. A missing file is handled below: the libc and Go
		// resolvers then fall back to localhost, which the filter denies.
		return nil
	}
	if hostResolverAllowedByEgressFilter(conf) {
		return nil
	}
	v4, _ := hysteria2ReadIPv4Routes()
	v6, _ := hysteria2ReadIPv6Routes()
	addr := hysteria2DoHResolverAddrV4
	if !hasIPv4DefaultRoute(v4) && hasIPv6DefaultRoute(v6) {
		addr = hysteria2DoHResolverAddrV6
	}
	return &hysteria2ResolverYAML{
		Type: "https",
		HTTPS: &hysteria2ResolverDoHYAML{
			Addr:    addr,
			Timeout: hysteria2DoHResolverTimeout,
			SNI:     hysteria2DoHResolverSNI,
		},
	}
}

// hostResolverAllowedByEgressFilter reports whether at least one resolv.conf
// nameserver is reachable from a unit under the egress filter: either it is
// a pierced resolved stub, or it is outside every denied range. An empty or
// missing config has no usable nameserver.
func hostResolverAllowedByEgressFilter(conf []byte) bool {
	stubs := map[netip.Addr]bool{}
	for _, cidr := range strings.Fields(egressAllowResolvedStub) {
		if p, err := netip.ParsePrefix(cidr); err == nil {
			stubs[p.Addr()] = true
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(conf))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}
		// A zoned link-local address (fe80::1%eth0) never matches a prefix,
		// so drop the zone before the containment check.
		addr = addr.WithZone("").Unmap()
		if stubs[addr] || !addrInEgressDeny(addr) {
			return true
		}
	}
	return false
}

func addrInEgressDeny(addr netip.Addr) bool {
	for _, cidr := range egressDenyCIDRs {
		if netip.MustParsePrefix(cidr).Contains(addr) {
			return true
		}
	}
	return false
}

// hasIPv4DefaultRoute parses /proc/net/route: a row whose Destination and
// Mask are both 00000000 is a default route.
func hasIPv4DefaultRoute(table []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(table))
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) >= 8 && f[1] == "00000000" && f[7] == "00000000" {
			return true
		}
	}
	return false
}

// hasIPv6DefaultRoute parses /proc/net/ipv6_route: a ::/0 row that is not
// on the loopback device (the kernel keeps a ::/0 unreachable route on lo).
func hasIPv6DefaultRoute(table []byte) bool {
	const zero = "00000000000000000000000000000000"
	scanner := bufio.NewScanner(bytes.NewReader(table))
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) >= 10 && f[0] == zero && f[1] == "00" && f[9] != "lo" {
			return true
		}
	}
	return false
}
