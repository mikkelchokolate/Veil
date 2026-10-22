package renderer

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
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
// forced onto plaintext :53. It is IPv6 on hosts with a route to the IPv6
// address and none to the IPv4 one, because hysteria accepts a single
// resolver address.
// Hysteria wraps the ACL engine with the resolver, so the reject() rules
// still match resolved addresses. The SOCKS5 (WARP) outbound ignores the
// resolve result and forwards the domain, so WARP-routed names are still
// resolved by sing-box.
const (
	filteredHostDoHAddrV4       = "1.1.1.1:443"
	filteredHostDoHAddrV6       = "[2606:4700:4700::1111]:443"
	filteredHostDoHSNI          = "cloudflare-dns.com"
	hysteria2DoHResolverTimeout = "10s"
)

// Host probes. Tests replace them; production reads the live host.
//
// Inside a test binary the probe is off unless a renderer test enables it
// (egressHostProbeInTests). Otherwise every package that renders through
// RenderHysteria2 or RenderWarpSingBox (protocols/hysteria2, generatedconfig,
// api, ...) would get output that depends on the machine's resolv.conf and
// routing table.
var (
	egressHostProbeInTests   = false
	egressHostReadResolvConf = func() ([]byte, error) { return os.ReadFile("/etc/resolv.conf") }
	egressHostReadIPv4Routes = func() ([]byte, error) { return os.ReadFile("/proc/net/route") }
	egressHostReadIPv6Routes = func() ([]byte, error) { return os.ReadFile("/proc/net/ipv6_route") }
	hysteria2ResolverProbe   = probeHysteria2Resolver
)

func probeHysteria2Resolver() *hysteria2ResolverYAML {
	addr, ok := filteredHostDoHAddr()
	if !ok {
		return nil
	}
	return &hysteria2ResolverYAML{
		Type: "https",
		HTTPS: &hysteria2ResolverDoHYAML{
			Addr:    addr,
			Timeout: hysteria2DoHResolverTimeout,
			SNI:     filteredHostDoHSNI,
		},
	}
}

// filteredHostDoHAddr is the host probe shared by every renderer whose daemon
// runs under the egress filter (Hysteria2 here, the WARP sing-box bridge in
// warp_resolver.go). It reports ok=false when the daemon should keep the host
// resolver: inside a test binary that has not opted in, when resolv.conf
// cannot be read for a reason other than absence, or when at least one
// nameserver is reachable under the filter. Otherwise it returns the public
// DoH address to pin, IPv6 only on hosts that route the IPv6 address and not
// the IPv4 one.
func filteredHostDoHAddr() (string, bool) {
	if testing.Testing() && !egressHostProbeInTests {
		return "", false
	}
	conf, err := egressHostReadResolvConf()
	if err != nil && !os.IsNotExist(err) {
		// Unknown host state: keep the daemon's default behaviour rather
		// than guess. A missing file is handled below: the libc and Go
		// resolvers then fall back to localhost, which the filter denies.
		return "", false
	}
	if hostResolverAllowedByEgressFilter(conf) {
		return "", false
	}
	return pickFilteredHostDoHAddr(), true
}

// pickFilteredHostDoHAddr returns the DoH address to pin on a host whose
// nameservers are all denied: IPv6 only when the host routes the IPv6
// address and not the IPv4 one.
func pickFilteredHostDoHAddr() string {
	v4, _ := egressHostReadIPv4Routes()
	v6, _ := egressHostReadIPv6Routes()
	if !hasIPv4RouteTo(v4, addrPortHost(filteredHostDoHAddrV4)) &&
		hasIPv6RouteTo(v6, addrPortHost(filteredHostDoHAddrV6)) {
		return filteredHostDoHAddrV6
	}
	return filteredHostDoHAddrV4
}

// hostResolverAllowedByEgressFilter reports whether at least one resolv.conf
// nameserver is reachable from a unit under the egress filter: either it is
// a pierced resolved stub, or it is outside every denied range. An empty or
// missing config has no usable nameserver.
func hostResolverAllowedByEgressFilter(conf []byte) bool {
	for _, addr := range resolvConfNameservers(conf) {
		if nameserverAllowedByEgressFilter(addr) {
			return true
		}
	}
	return false
}

// resolvConfNameservers returns the nameservers the libc and Go resolvers
// actually query: the first three parseable "nameserver" lines (Go's
// dnsconfig_unix.go and glibc MAXNS), in file order and as written (a zoned
// link-local address keeps its zone).
func resolvConfNameservers(conf []byte) []netip.Addr {
	const maxNameservers = 3
	var out []netip.Addr
	scanner := bufio.NewScanner(bytes.NewReader(conf))
	for scanner.Scan() && len(out) < maxNameservers {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}
		out = append(out, addr)
	}
	return out
}

// nameserverAllowedByEgressFilter reports whether a unit under the egress
// filter can reach addr: it is a pierced resolved stub, or it is outside
// every denied range.
func nameserverAllowedByEgressFilter(addr netip.Addr) bool {
	// A zoned link-local address (fe80::1%eth0) never matches a prefix,
	// so drop the zone before the containment check.
	addr = addr.WithZone("").Unmap()
	for _, cidr := range strings.Fields(egressAllowResolvedStub) {
		if p, err := netip.ParsePrefix(cidr); err == nil && p.Addr() == addr {
			return true
		}
	}
	return !addrInEgressDeny(addr)
}

func addrInEgressDeny(addr netip.Addr) bool {
	for _, cidr := range egressDenyCIDRs {
		if netip.MustParsePrefix(cidr).Contains(addr) {
			return true
		}
	}
	return false
}

func addrPortHost(hostport string) netip.Addr {
	return netip.MustParseAddrPort(hostport).Addr()
}

const (
	routeFlagUp     = 0x0001
	routeFlagReject = 0x0200
)

// hasIPv4RouteTo parses /proc/net/route and reports whether a usable route
// covers target. Any covering route counts, not just 0.0.0.0/0, so the
// 0.0.0.0/1 + 128.0.0.0/1 pair installed by OpenVPN def1 (or wg-quick on
// macOS/BSD) counts as IPv4 reachability. Destination and Mask are printed
// as host-order u32s.
//
// Both /proc route files show only the main table. Linux wg-quick and other
// policy routing keep the main-table default, so this is enough in practice;
// a host whose only default lives in a non-main table is misjudged and gets
// the IPv4 address.
func hasIPv4RouteTo(table []byte, target netip.Addr) bool {
	t := target.As4()
	scanner := bufio.NewScanner(bytes.NewReader(table))
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) < 8 {
			continue
		}
		dest, okDest := procRouteIPv4(f[1])
		mask, okMask := procRouteIPv4(f[7])
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if !okDest || !okMask || err != nil || flags&routeFlagUp == 0 || flags&routeFlagReject != 0 {
			continue
		}
		covered := true
		for i := range t {
			if t[i]&mask[i] != dest[i] {
				covered = false
				break
			}
		}
		if covered {
			return true
		}
	}
	return false
}

func procRouteIPv4(field string) ([4]byte, bool) {
	var b [4]byte
	v, err := strconv.ParseUint(field, 16, 32)
	if err != nil || len(field) != 8 {
		return b, false
	}
	binary.NativeEndian.PutUint32(b[:], uint32(v))
	return b, true
}

// hasIPv6RouteTo parses /proc/net/ipv6_route (network-order destination,
// hex prefix length, flags, device) and reports whether a usable non-lo
// route covers target. The kernel keeps an unreachable ::/0 on lo, which is
// skipped by the reject flag and the device check.
func hasIPv6RouteTo(table []byte, target netip.Addr) bool {
	scanner := bufio.NewScanner(bytes.NewReader(table))
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) < 10 || f[9] == "lo" {
			continue
		}
		raw, err := hex.DecodeString(f[0])
		if err != nil || len(raw) != 16 {
			continue
		}
		bits, err := strconv.ParseUint(f[1], 16, 8)
		if err != nil || bits > 128 {
			continue
		}
		flags, err := strconv.ParseUint(f[8], 16, 32)
		if err != nil || flags&routeFlagUp == 0 || flags&routeFlagReject != 0 {
			continue
		}
		prefix, err := netip.AddrFrom16([16]byte(raw)).Prefix(int(bits))
		if err == nil && prefix.Contains(target) {
			return true
		}
	}
	return false
}
