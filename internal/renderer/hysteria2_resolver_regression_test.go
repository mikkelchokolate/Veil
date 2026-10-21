package renderer

import (
	"io/fs"
	"net"
	"net/netip"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	testIPv4DefaultRoute = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0100000A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"eth0\t0000000A\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n"
	testIPv4NoDefault = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth0\t0000000A\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n"
	// The kernel always keeps an unreachable ::/0 on lo; only a non-lo ::/0
	// row is a real default route.
	testIPv6LoopbackOnly = "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200       lo\n"
	testIPv6DefaultRoute = testIPv6LoopbackOnly +
		"00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000002 00000000 00450003     eth0\n"
)

func withHysteria2HostProbe(t *testing.T, resolvConf string, confErr error, v4, v6 string) {
	t.Helper()
	oldConf, oldV4, oldV6 := hysteria2ReadResolvConf, hysteria2ReadIPv4Routes, hysteria2ReadIPv6Routes
	t.Cleanup(func() {
		hysteria2ReadResolvConf, hysteria2ReadIPv4Routes, hysteria2ReadIPv6Routes = oldConf, oldV4, oldV6
	})
	hysteria2ReadResolvConf = func() ([]byte, error) { return []byte(resolvConf), confErr }
	hysteria2ReadIPv4Routes = func() ([]byte, error) { return []byte(v4), nil }
	hysteria2ReadIPv6Routes = func() ([]byte, error) { return []byte(v6), nil }
}

type hysteria2ResolverDoc struct {
	Resolver *struct {
		Type  string `yaml:"type"`
		HTTPS struct {
			Addr     string `yaml:"addr"`
			Timeout  string `yaml:"timeout"`
			SNI      string `yaml:"sni"`
			Insecure bool   `yaml:"insecure"`
		} `yaml:"https"`
	} `yaml:"resolver"`
}

func renderHysteria2ResolverDoc(t *testing.T, cfg Hysteria2Config) hysteria2ResolverDoc {
	t.Helper()
	body, err := RenderHysteria2(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc hysteria2ResolverDoc
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("rendered config is not YAML: %v\n%s", err, body)
	}
	return doc
}

// #1288: Hysteria2 runs under IPAddressDeny= covering every private,
// loopback, link-local and CGNAT range, with only the resolved stubs
// pierced. A host whose resolv.conf names no reachable nameserver must get a
// pinned public DoH resolver. A host that already resolves through a
// reachable nameserver keeps its system resolver.
func TestRenderHysteria2ResolverFollowsHostNameserverReachability(t *testing.T) {
	cases := []struct {
		name    string
		conf    string
		confErr error
		v4, v6  string
		want    string // "" = no resolver block (system resolver kept)
	}{
		{name: "resolved stub", conf: "nameserver 127.0.0.53\noptions edns0 trust-ad\n", v4: testIPv4DefaultRoute},
		{name: "resolved stub 127.0.0.54", conf: "nameserver 127.0.0.54\n", v4: testIPv4DefaultRoute},
		{name: "public nameserver", conf: "nameserver 185.12.64.1\n", v4: testIPv4DefaultRoute},
		{name: "public IPv6 nameserver", conf: "nameserver 2a01:4ff:ff00::add:1\n", v6: testIPv6DefaultRoute},
		{name: "IPv6-only host on resolved stub", conf: "nameserver 127.0.0.53\n", v4: testIPv4NoDefault, v6: testIPv6DefaultRoute},
		{name: "one reachable among denied", conf: "nameserver 10.0.0.2\nnameserver 1.1.1.1\n", v4: testIPv4DefaultRoute},
		{name: "unreadable resolv.conf", confErr: fs.ErrPermission, v4: testIPv4DefaultRoute},

		{name: "AWS VPC resolver", conf: "search ec2.internal\nnameserver 172.31.0.2\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "AWS link-local resolver", conf: "nameserver 169.254.169.253\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "Alibaba CGNAT resolver", conf: "nameserver 100.100.2.136\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "local dnsmasq", conf: "nameserver 127.0.0.1\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "zoned IPv6 link-local", conf: "nameserver fe80::1%eth0\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "IPv4-mapped loopback", conf: "nameserver ::ffff:127.0.0.1\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "commented-out public nameserver", conf: "# nameserver 1.1.1.1\nnameserver 192.168.1.1\n", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "no resolv.conf", confErr: fs.ErrNotExist, v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "empty resolv.conf", conf: "", v4: testIPv4DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "dual-stack denied nameserver", conf: "nameserver 10.0.0.2\n", v4: testIPv4DefaultRoute, v6: testIPv6DefaultRoute, want: hysteria2DoHResolverAddrV4},
		{name: "IPv6-only host denied nameserver", conf: "nameserver fd00::53\n", v4: testIPv4NoDefault, v6: testIPv6DefaultRoute, want: hysteria2DoHResolverAddrV6},
		{name: "only lo ::/0 is not IPv6 default", conf: "nameserver 10.0.0.2\n", v4: testIPv4NoDefault, v6: testIPv6LoopbackOnly, want: hysteria2DoHResolverAddrV4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withHysteria2HostProbe(t, tc.conf, tc.confErr, tc.v4, tc.v6)
			doc := renderHysteria2ResolverDoc(t, Hysteria2Config{ListenPort: 443, Password: "secret"})
			if tc.want == "" {
				if doc.Resolver != nil {
					t.Fatalf("resolver rendered for a host whose own resolver is reachable: %+v", *doc.Resolver)
				}
				return
			}
			if doc.Resolver == nil {
				t.Fatal("no resolver rendered: every lookup would be dropped by the unit's egress filter")
			}
			if doc.Resolver.Type != "https" || doc.Resolver.HTTPS.Addr != tc.want {
				t.Fatalf("resolver = %s %s, want https %s", doc.Resolver.Type, doc.Resolver.HTTPS.Addr, tc.want)
			}
			if doc.Resolver.HTTPS.SNI != hysteria2DoHResolverSNI || doc.Resolver.HTTPS.Insecure || doc.Resolver.HTTPS.Timeout == "" {
				t.Fatalf("resolver TLS settings = %+v", doc.Resolver.HTTPS)
			}
		})
	}
}

// The resolver choice must not depend on the auth mode or the WARP upstream.
func TestRenderHysteria2ResolverInEveryRenderMode(t *testing.T) {
	withHysteria2HostProbe(t, "nameserver 172.31.0.2\n", nil, testIPv4DefaultRoute, "")
	for name, cfg := range map[string]Hysteria2Config{
		"password":      {ListenPort: 443, Password: "secret"},
		"userpass":      {ListenPort: 443, Users: []Hysteria2User{{Username: "a", Password: "b"}}},
		"http auth":     {ListenPort: 443, Password: "secret", HTTPAuthURL: "http://127.0.0.1:2096/internal/hysteria2/auth"},
		"warp upstream": {ListenPort: 443, Password: "secret", Upstream: "127.41.0.1:40000"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := renderHysteria2ResolverDoc(t, cfg)
			if doc.Resolver == nil || doc.Resolver.HTTPS.Addr != hysteria2DoHResolverAddrV4 {
				t.Fatalf("resolver = %+v, want DoH %s", doc.Resolver, hysteria2DoHResolverAddrV4)
			}
		})
	}
}

// Both pinned DoH addresses must be literal public IPs on 443 that the
// unit filter allows. A hostname would need a bootstrap lookup through the
// denied system resolver.
func TestHysteria2DoHResolverAddrsPassEgressFilter(t *testing.T) {
	for _, hostport := range []string{hysteria2DoHResolverAddrV4, hysteria2DoHResolverAddrV6} {
		host, port, err := net.SplitHostPort(hostport)
		if err != nil {
			t.Fatalf("%q is not host:port: %v", hostport, err)
		}
		if p, err := strconv.Atoi(port); err != nil || p != 443 {
			t.Fatalf("%q port = %q, want 443", hostport, port)
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			t.Fatalf("%q host must be a literal IP: %v", hostport, err)
		}
		if addrInEgressDeny(addr) {
			t.Fatalf("%s falls inside the egress deny set", addr)
		}
	}
}
