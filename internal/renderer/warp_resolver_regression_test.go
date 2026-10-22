package renderer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func warpResolverTestConfig() WarpSingBoxConfig {
	return WarpSingBoxConfig{
		PrivateKey:    "YFYOAdbw1bKTHlNNi+aEjBM3BO7unuFC5rOkMRAz9XY=",
		LocalAddress:  "172.16.0.2/32,2606:4700:110:8a36:df92:102a:9602:fa18/128",
		PeerPublicKey: "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
		RoutingRules:  []WarpRoutingRule{{Match: "geosite:ru-blocked", Outbound: "warp"}, {Match: "all", Outbound: "direct"}},
	}
}

type warpResolverDoc struct {
	DNS *struct {
		Servers []struct {
			Type       string `json:"type"`
			Tag        string `json:"tag"`
			Server     string `json:"server"`
			ServerPort int    `json:"server_port"`
			Detour     string `json:"detour"`
			TLS        struct {
				Enabled    bool   `json:"enabled"`
				ServerName string `json:"server_name"`
				Insecure   bool   `json:"insecure"`
			} `json:"tls"`
		} `json:"servers"`
		Final string `json:"final"`
	} `json:"dns"`
	Route struct {
		DefaultDomainResolver *struct {
			Server string `json:"server"`
		} `json:"default_domain_resolver"`
	} `json:"route"`
}

func renderWarpResolverDoc(t *testing.T) (string, warpResolverDoc) {
	t.Helper()
	body, err := RenderWarpSingBox(warpResolverTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	var doc warpResolverDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("decode rendered sing-box config: %v\n%s", err, body)
	}
	return body, doc
}

// #1288: veil-warp runs under the same IPAddressDeny= filter as Hysteria2.
// Without a "dns" section sing-box resolves through /etc/resolv.conf, so on a
// host whose nameservers are all denied the WireGuard peer hostname, every
// client domain and the rule-set downloads fail. Such a host must get a
// pinned public DoH server that is dialed directly and is the default domain
// resolver. A host that already reaches its resolver keeps it, and its config
// is byte-identical to a render with the probe off.
func TestRenderWarpSingBoxResolverFollowsHostNameserverReachability(t *testing.T) {
	baseline, _ := renderWarpResolverDoc(t)
	cases := []struct {
		name       string
		conf       string
		v4, v6     string
		wantServer string // "" = no dns section (host resolver kept)
	}{
		{name: "resolved stub", conf: "nameserver 127.0.0.53\n", v4: testIPv4DefaultRoute},
		{name: "public nameserver", conf: "nameserver 185.12.64.1\n", v4: testIPv4DefaultRoute},
		{name: "one reachable among denied", conf: "nameserver 10.0.0.2\nnameserver 1.1.1.1\n", v4: testIPv4DefaultRoute},

		{name: "AWS VPC resolver", conf: "nameserver 172.31.0.2\n", v4: testIPv4DefaultRoute, wantServer: "1.1.1.1"},
		{name: "GCP/OCI link-local resolver", conf: "nameserver 169.254.169.254\n", v4: testIPv4DefaultRoute, wantServer: "1.1.1.1"},
		{name: "local dnsmasq", conf: "nameserver 127.0.0.1\n", v4: testIPv4DefaultRoute, wantServer: "1.1.1.1"},
		{name: "IPv6-only host denied nameserver", conf: "nameserver fd00::53\n", v4: testIPv4NoDefault, v6: testIPv6DefaultRoute, wantServer: "2606:4700:4700::1111"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEgressHostProbe(t, tc.conf, nil, tc.v4, tc.v6)
			body, doc := renderWarpResolverDoc(t)
			if tc.wantServer == "" {
				if body != baseline {
					t.Fatalf("config changed for a host whose own resolver is reachable:\n%s", body)
				}
				return
			}
			if doc.DNS == nil || len(doc.DNS.Servers) != 1 {
				t.Fatalf("dns = %+v, want exactly one pinned DoH server: every lookup would be dropped by the unit's egress filter", doc.DNS)
			}
			server := doc.DNS.Servers[0]
			if server.Type != "https" || server.Server != tc.wantServer || server.ServerPort != 443 {
				t.Fatalf("dns server = %s %s:%d, want https %s:443", server.Type, server.Server, server.ServerPort, tc.wantServer)
			}
			// A detour would send the lookup through an outbound; through
			// "warp" it could never resolve the peer that brings WARP up.
			if server.Detour != "" {
				t.Fatalf("dns server detour = %q, want direct dial", server.Detour)
			}
			if !server.TLS.Enabled || server.TLS.Insecure || server.TLS.ServerName != filteredHostDoHSNI {
				t.Fatalf("dns server TLS = %+v", server.TLS)
			}
			if doc.DNS.Final != server.Tag {
				t.Fatalf("dns final = %q, want %q", doc.DNS.Final, server.Tag)
			}
			if doc.Route.DefaultDomainResolver == nil || doc.Route.DefaultDomainResolver.Server != server.Tag {
				t.Fatalf("route.default_domain_resolver = %+v, want %q: the WireGuard peer and direct outbounds would still use resolv.conf", doc.Route.DefaultDomainResolver, server.Tag)
			}
		})
	}
}

// Packages that render the WARP config inside a test binary must not see the
// machine's resolv.conf unless a renderer test opts in.
func TestRenderWarpSingBoxHostProbeOffInOtherTests(t *testing.T) {
	withEgressHostProbe(t, "nameserver 172.31.0.2\n", nil, testIPv4DefaultRoute, "")
	egressHostProbeInTests = false
	if _, doc := renderWarpResolverDoc(t); doc.DNS != nil || doc.Route.DefaultDomainResolver != nil {
		t.Fatalf("host probe ran inside a test binary without opting in: dns=%+v resolver=%+v", doc.DNS, doc.Route.DefaultDomainResolver)
	}
}

// The pinned DNS section must pass the pinned sing-box's own validation, the
// same `sing-box check` that gates the generated warp.json. CI carries the
// binary; the test skips where it is absent.
func TestRenderWarpSingBoxPinnedResolverPassesSingBoxCheck(t *testing.T) {
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box not on PATH")
	}
	for name, v6 := range map[string]bool{"ipv4": false, "ipv6": true} {
		t.Run(name, func(t *testing.T) {
			if v6 {
				withEgressHostProbe(t, "nameserver fd00::53\n", nil, testIPv4NoDefault, testIPv6DefaultRoute)
			} else {
				withEgressHostProbe(t, "nameserver 172.31.0.2\n", nil, testIPv4DefaultRoute, "")
			}
			body, doc := renderWarpResolverDoc(t)
			if doc.DNS == nil {
				t.Fatal("probe did not pin a resolver")
			}
			path := filepath.Join(t.TempDir(), "warp.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(bin, "check", "-c", path).CombinedOutput(); err != nil {
				t.Fatalf("sing-box check: %v\n%s\n%s", err, out, body)
			}
		})
	}
}
