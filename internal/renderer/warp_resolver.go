package renderer

import "net/netip"

// The WARP sing-box bridge (veil-warp) runs under the same kernel egress
// filter as Hysteria2: IPAddressDeny= every private, loopback, link-local and
// CGNAT range, with only the systemd-resolved stubs pierced. With no "dns"
// section sing-box resolves through its local transport, i.e. /etc/resolv.conf.
// On a host whose nameservers are all inside a denied range (AWS VPC
// 172.31.0.2, 169.254.169.253/.254 on AWS/GCP/OCI, Alibaba 100.100.2.136,
// LXC/home-lab 10.x/192.168.x, a local dnsmasq on 127.0.0.1), every lookup is
// dropped. That breaks the WireGuard peer itself (engage.cloudflareclient.com
// is resolved through the default domain resolver), so the tunnel never comes
// up, along with every domain a client sends to the SOCKS inbound and the
// remote rule-set downloads (#1288).
//
// Only those hosts get a pinned public DNS-over-HTTPS server, chosen by the
// same probe as Hysteria2 (filteredHostDoHAddr). A host whose resolv.conf
// already names a reachable nameserver renders byte-identically to before.
// The server is a literal IP, so it needs no bootstrap lookup, and it has no
// detour, so sing-box dials it directly (sing-box >= 1.12 DNS servers default
// to an empty direct dialer) instead of through the tunnel it is needed to
// bring up. It is both the DNS router's only server, so it answers lookups
// for WARP-routed destinations, and route.default_domain_resolver, so the
// WireGuard peer and the direct outbounds resolve through it.
const warpDoHServerTag = "veil-doh"

var warpDNSResolverProbe = filteredHostDoHAddr

// warpDNSOverride returns the sing-box "dns" section and the
// route.default_domain_resolver value to pin, or ok=false when the host
// resolver is reachable under the egress filter.
func warpDNSOverride() (dns map[string]any, defaultDomainResolver map[string]any, ok bool) {
	addr, ok := warpDNSResolverProbe()
	if !ok {
		return nil, nil, false
	}
	parsed, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, nil, false
	}
	server := map[string]any{
		"type":        "https",
		"tag":         warpDoHServerTag,
		"server":      parsed.Addr().String(),
		"server_port": int(parsed.Port()),
		"tls": map[string]any{
			"enabled":     true,
			"server_name": filteredHostDoHSNI,
		},
	}
	dns = map[string]any{
		"servers": []map[string]any{server},
		"final":   warpDoHServerTag,
	}
	return dns, map[string]any{"server": warpDoHServerTag}, true
}
