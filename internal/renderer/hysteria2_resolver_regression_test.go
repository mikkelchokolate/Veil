package renderer

import (
	"net"
	"net/netip"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

// #1288: Hysteria2 runs under IPAddressDeny= covering every private,
// loopback, link-local and CGNAT range, so it must never fall back to the
// /etc/resolv.conf nameserver (AWS 172.31.0.2, GCP 169.254.169.254, a
// local dnsmasq on 127.0.0.1, ...). Every render mode must pin an explicit
// resolver whose address is a literal public IP the filter allows.
func TestRenderHysteria2PinsPublicResolverInsideEgressFilter(t *testing.T) {
	cases := map[string]Hysteria2Config{
		"password": {ListenPort: 443, Password: "secret"},
		"userpass": {ListenPort: 443, Users: []Hysteria2User{{Username: "a", Password: "b"}}},
		"http auth": {
			ListenPort:  443,
			Password:    "secret",
			HTTPAuthURL: "http://127.0.0.1:2096/internal/hysteria2/auth",
		},
		"warp upstream": {ListenPort: 443, Password: "secret", Upstream: "127.41.0.1:40000"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			body, err := RenderHysteria2(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Resolver struct {
					Type  string `yaml:"type"`
					HTTPS struct {
						Addr     string `yaml:"addr"`
						Timeout  string `yaml:"timeout"`
						SNI      string `yaml:"sni"`
						Insecure bool   `yaml:"insecure"`
					} `yaml:"https"`
				} `yaml:"resolver"`
			}
			if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
				t.Fatalf("rendered config is not YAML: %v\n%s", err, body)
			}
			if doc.Resolver.Type != "https" {
				t.Fatalf("resolver.type = %q, want https (system resolver is dropped by the egress filter)\n%s", doc.Resolver.Type, body)
			}
			r := doc.Resolver.HTTPS
			host, port, err := net.SplitHostPort(r.Addr)
			if err != nil {
				t.Fatalf("resolver.https.addr %q is not host:port: %v", r.Addr, err)
			}
			if p, err := strconv.Atoi(port); err != nil || p != 443 {
				t.Fatalf("resolver.https.addr port = %q, want 443", port)
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				t.Fatalf("resolver.https.addr host %q must be a literal IP (a hostname needs a bootstrap lookup through the denied system resolver)", host)
			}
			for _, cidr := range egressDenyCIDRs {
				if netip.MustParsePrefix(cidr).Contains(addr) {
					t.Fatalf("resolver %s falls inside egress deny %s: the unit filter would drop every lookup", addr, cidr)
				}
			}
			if r.SNI == "" {
				t.Fatal("resolver.https.sni is empty: DoH certificate verification needs the server name")
			}
			if r.Insecure {
				t.Fatal("resolver.https.insecure must stay false")
			}
			if r.Timeout == "" {
				t.Fatal("resolver.https.timeout is empty")
			}
		})
	}
}
