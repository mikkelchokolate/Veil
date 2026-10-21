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
	hysteria2DoHResolverAddrV4  = "1.1.1.1:443"
	hysteria2DoHResolverAddrV6  = "[2606:4700:4700::1111]:443"
	hysteria2DoHResolverSNI     = "cloudflare-dns.com"
	hysteria2DoHResolverTimeout = "10s"
)

// Host probes. Tests replace them; production reads the live host.
//
// Inside a test binary the probe is off unless a renderer test enables it
// (hysteria2ProbeHostInTests). Otherwise every package that renders through
// RenderHysteria2 (protocols/hysteria2, generatedconfig, api, ...) would get
// output that depends on the machine's resolv.conf and routing table.
var (
	hysteria2ProbeHostInTests = false
	hysteria2ReadResolvConf   = func() ([]byte, error) { return os.ReadFile("/etc/resolv.conf") }
	hysteria2ReadIPv4Routes   = func() ([]byte, error) { return os.ReadFile("/proc/net/route") }
	hysteria2ReadIPv6Routes   = func() ([]byte, error) { return os.ReadFile("/proc/net/ipv6_route") }
	hysteria2ResolverProbe    = probeHysteria2Resolver
)

func probeHysteria2Resolver() *hysteria2ResolverYAML {
	if testing.Testing() && !hysteria2ProbeHostInTests {
		return nil
	}
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
	if !hasIPv4RouteTo(v4, addrPortHost(hysteria2DoHResolverAddrV4)) &&
		hasIPv6RouteTo(v6, addrPortHost(hysteria2DoHResolverAddrV6)) {
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
	// Go's resolver (dnsconfig_unix.go) and glibc (MAXNS) use only the first
	// three parseable nameserver lines; a reachable fourth is never queried.
	const maxNameservers = 3
	seen := 0
	scanner := bufio.NewScanner(bytes.NewReader(conf))
	for scanner.Scan() && seen < maxNameservers {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}
		seen++
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

func addrPortHost(hostport string) netip.Addr {
	return netip.MustParseAddrPort(hostport).Addr()
}

const (
	routeFlagUp     = 0x0001
	routeFlagReject = 0x0200
)

// hasIPv4RouteTo parses /proc/net/route and reports whether a usable route
// covers target. Any covering route counts, not just 0.0.0.0/0, so the
// 0.0.0.0/1 + 128.0.0.0/1 pair installed by OpenVPN def1 or wg-quick counts
// as IPv4 reachability. Destination and Mask are printed as host-order u32s.
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
