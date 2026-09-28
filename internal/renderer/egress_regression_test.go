package renderer

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"gopkg.in/yaml.v3"
)

// #1095: without an ACL Hysteria2 proxies to ANY destination the host can
// reach — including the unauthenticated Caddy admin API on 127.0.0.1:2019.
// The ACL is mandatory even with no WARP upstream, and the leading reject
// block must precede every operator rule so no user route reopens a denied
// range.
func TestRenderHysteria2EmitsDenyFirstACLWithoutUpstream(t *testing.T) {
	body, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Password:   "secret",
		RoutingRules: []Hysteria2RoutingRule{
			{Match: "suffix:example.com", Outbound: "direct"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []struct {
			Name string `yaml:"name"`
		} `yaml:"outbounds"`
		ACL struct {
			Inline []string `yaml:"inline"`
		} `yaml:"acl"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("rendered config is not YAML: %v\n%s", err, body)
	}
	if len(doc.ACL.Inline) == 0 {
		t.Fatalf("no acl.inline rendered without upstream (open egress to loopback):\n%s", body)
	}
	for i, cidr := range egressDenyCIDRs {
		if doc.ACL.Inline[i] != "reject("+cidr+")" {
			t.Fatalf("acl.inline[%d] = %q, want reject(%s)\ninline: %v", i, doc.ACL.Inline[i], cidr, doc.ACL.Inline)
		}
	}
	if got := doc.ACL.Inline[len(doc.ACL.Inline)-1]; got != "direct(all)" {
		t.Fatalf("final ACL rule = %q, want direct(all) without an upstream", got)
	}
	// The operator rule must sit strictly after the deny block.
	if doc.ACL.Inline[len(egressDenyCIDRs)] != "direct(suffix:example.com)" {
		t.Fatalf("operator rule did not follow the deny block: %v", doc.ACL.Inline)
	}
	for _, outbound := range doc.Outbounds {
		if outbound.Name == "warp" {
			t.Fatalf("warp outbound emitted without an upstream: %v", doc.Outbounds)
		}
	}
	if len(doc.Outbounds) != 2 {
		t.Fatalf("outbounds = %v, want [direct proxy]", doc.Outbounds)
	}
}

func TestRenderHysteria2DenyBlockPrecedesWarpFinalWithUpstream(t *testing.T) {
	body, err := RenderHysteria2(Hysteria2Config{
		ListenPort: 443,
		Password:   "secret",
		Upstream:   "127.41.0.1:40000",
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []struct {
			Name string `yaml:"name"`
		} `yaml:"outbounds"`
		ACL struct {
			Inline []string `yaml:"inline"`
		} `yaml:"acl"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("rendered config is not YAML: %v\n%s", err, body)
	}
	for i, cidr := range egressDenyCIDRs {
		if len(doc.ACL.Inline) <= i || doc.ACL.Inline[i] != "reject("+cidr+")" {
			t.Fatalf("acl.inline[%d] = %q, want reject(%s)", i, doc.ACL.Inline[i], cidr)
		}
	}
	if got := doc.ACL.Inline[len(doc.ACL.Inline)-1]; got != "warp(all)" {
		t.Fatalf("final ACL rule = %q, want warp(all) with an upstream", got)
	}
	warp := false
	for _, outbound := range doc.Outbounds {
		if outbound.Name == "warp" {
			warp = true
		}
	}
	if !warp {
		t.Fatalf("warp outbound missing with upstream: %v", doc.Outbounds)
	}
}

// #1096: NaiveProxy with a WARP upstream makes Caddy dial the local sing-box
// SOCKS bridge, and the pinned forwardproxy fork skips its own ACL when
// upstreaming — so sing-box is the choke point and must reject restricted
// destinations before any user rule (e.g. ip_is_private → direct).
func TestRenderWarpSingBoxLeadingDenyRulePrecedesUserRules(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		PrivateKey:    "priv",
		LocalAddress:  "172.16.0.2/32,2606:4700:110:8a36::/128",
		PeerPublicKey: "peer",
		RoutingRules: []WarpRoutingRule{
			{Match: "geoip:private", Outbound: "direct"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []struct {
			Type string `json:"type"`
			Tag  string `json:"tag"`
		} `json:"outbounds"`
		Route struct {
			Rules []struct {
				Outbound string   `json:"outbound"`
				IPCIDR   []string `json:"ip_cidr"`
			} `json:"rules"`
			Final string `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("warp config is not JSON: %v\n%s", err, body)
	}
	blockFound := false
	for _, outbound := range doc.Outbounds {
		if outbound.Type == "block" && outbound.Tag == "block" {
			blockFound = true
		}
	}
	if !blockFound {
		t.Fatalf("no block outbound rendered:\n%s", body)
	}
	if len(doc.Route.Rules) < 2 {
		t.Fatalf("route.rules = %+v, want leading deny + user rule", doc.Route.Rules)
	}
	lead := doc.Route.Rules[0]
	if lead.Outbound != "block" {
		t.Fatalf("route.rules[0].outbound = %q, want block", lead.Outbound)
	}
	if len(lead.IPCIDR) != len(egressDenyCIDRs) {
		t.Fatalf("leading deny ip_cidr = %v, want %d entries", lead.IPCIDR, len(egressDenyCIDRs))
	}
	for i, cidr := range egressDenyCIDRs {
		if lead.IPCIDR[i] != cidr {
			t.Fatalf("leading deny ip_cidr[%d] = %q, want %q", i, lead.IPCIDR[i], cidr)
		}
	}
	if doc.Route.Rules[1].Outbound != "direct" {
		t.Fatalf("user rule must follow the deny rule: %+v", doc.Route.Rules)
	}
	if doc.Route.Final != "warp" {
		t.Fatalf("route.final = %q, want warp", doc.Route.Final)
	}
}

// #1096/#1097: Caddy's forward_proxy must carry an explicit deny ACL — its
// built-in private-range defaults miss unspecified/CGNAT/metadata/mapped
// space, and the ACL must be present in BOTH render paths (Caddyfile and the
// JSON catalog) because only one of them ships on a given install.
func TestRenderNaiveCaddyfileCarriesEgressDenyACL(t *testing.T) {
	body, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:       "vpn.example.com",
		Username:     "u",
		Password:     "p",
		ListenPort:   443,
		FallbackRoot: "/etc/veil/www/fallback",
		Upstream:     "socks5://127.41.0.1:40000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "acl {") {
		t.Fatalf("no acl block in Caddyfile:\n%s", body)
	}
	for _, cidr := range egressDenyCIDRs {
		if !strings.Contains(body, "deny") || !strings.Contains(body, cidr) {
			t.Fatalf("acl deny missing %q:\n%s", cidr, body)
		}
	}
}

func TestRenderCaddyJSONForwardProxyDenyACL(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "p.example.com",
				InboundName: "naive-1",
				Transport:   "tcp",
				Upstream:    "socks5://127.41.0.1:40000",
				NaiveUsers:  []caddyassembly.CaddyNaiveUser{{Username: "u", Password: "p"}},
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	body, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Routes []struct {
						Handle []struct {
							Handler         string           `json:"handler"`
							ACL             []map[string]any `json:"acl"`
							Upstream        string           `json:"upstream"`
							AuthCredentials []string         `json:"auth_credentials"`
						} `json:"handle"`
					} `json:"routes"`
				} `json:"servers"`
			} `json:"http"`
		} `json:"apps"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("caddy JSON not parseable: %v", err)
	}
	found := false
	for _, server := range doc.Apps.HTTP.Servers {
		for _, route := range server.Routes {
			for _, handle := range route.Handle {
				if handle.Handler != "forward_proxy" {
					continue
				}
				found = true
				if len(handle.ACL) != 1 || handle.ACL[0]["allow"] != false {
					t.Fatalf("forward_proxy acl = %+v, want one deny entry", handle.ACL)
				}
				subjects, _ := handle.ACL[0]["subjects"].([]any)
				if len(subjects) != len(egressDenyCIDRs) {
					t.Fatalf("forward_proxy acl subjects = %v, want %d CIDRs", subjects, len(egressDenyCIDRs))
				}
			}
		}
	}
	if !found {
		t.Fatal("no forward_proxy handler found in rendered caddy JSON")
	}
}

// #1097: mita's built-in egress guard only rejects private/loopback literal
// IPs and a handful of localhost names — link-local/CGNAT/ULA/multicast and
// unspecified literals pass through as DIRECT. The rendered egress rules close
// the literal-IP gap; resolved-name bypasses are covered by the unit filter.
func TestRenderMieruEmitsEgressRejectRule(t *testing.T) {
	body, err := RenderMieru(MieruConfig{
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		Users:        []MieruUser{{Name: "u", Password: "p"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Egress struct {
			Rules []struct {
				IPRanges []string `json:"ipRanges"`
				Action   string   `json:"action"`
			} `json:"rules"`
		} `json:"egress"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("mieru config is not JSON: %v\n%s", err, body)
	}
	if len(doc.Egress.Rules) != 1 || doc.Egress.Rules[0].Action != "REJECT" {
		t.Fatalf("egress.rules = %+v, want one REJECT rule", doc.Egress.Rules)
	}
	if len(doc.Egress.Rules[0].IPRanges) != len(egressDenyCIDRs) {
		t.Fatalf("ipRanges = %v, want %d CIDRs", doc.Egress.Rules[0].IPRanges, len(egressDenyCIDRs))
	}
}

// #1097: the protocol daemons' units get a kernel-level allow-before-deny IP
// filter so no rendered-ACL regression can reopen loopback/private/link-local
// egress. The veil-caddy unit is deliberately NOT filtered the same way — it
// must reverse-proxy to the panel backend on loopback.
func TestSystemdProxyUnitsCarryEgressIPFilter(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})
	filtered := []string{UnitHysteria2, UnitMieru, UnitOlcrtc, UnitWarp}
	for _, name := range filtered {
		body, ok := units[name]
		if !ok {
			t.Fatalf("unit %s not rendered", name)
		}
		var allow, deny string
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "IPAddressAllow=") {
				allow = strings.TrimPrefix(line, "IPAddressAllow=")
			}
			if strings.HasPrefix(line, "IPAddressDeny=") {
				deny = strings.TrimPrefix(line, "IPAddressDeny=")
			}
		}
		if allow == "" || deny == "" {
			t.Fatalf("%s missing IPAddressAllow/IPAddressDeny:\n%s", name, body)
		}
		for _, cidr := range egressDenyCIDRs {
			if !strings.Contains(" "+deny+" ", " "+cidr+" ") {
				t.Fatalf("%s IPAddressDeny missing %q:\n%s", name, cidr, body)
			}
		}
		for _, stub := range []string{"127.0.0.53/32", "127.0.0.54/32"} {
			if !strings.Contains(allow, stub) {
				t.Fatalf("%s IPAddressAllow missing resolved stub %q", name, stub)
			}
		}
	}
	// Per-unit carve-outs: the WARP SOCKS band is only allowed where a daemon
	// legitimately dials it, and the stats band only on the hysteria2 unit.
	if !strings.Contains(units[UnitHysteria2], "IPAddressAllow=127.0.0.53/32 127.0.0.54/32 127.40.0.0/16 127.41.0.0/16") {
		t.Fatalf("hysteria2 allow list missing stats+warp bands:\n%s", units[UnitHysteria2])
	}
	if !strings.Contains(units[UnitOlcrtc], "127.41.0.0/16") || strings.Contains(units[UnitOlcrtc], "127.40.0.0/16") {
		t.Fatalf("olcRTC allow list must carry the warp band only:\n%s", units[UnitOlcrtc])
	}
	if strings.Contains(units[UnitMieru], "127.41.0.0/16") || strings.Contains(units[UnitMieru], "127.40.0.0/16") {
		t.Fatalf("mieru allow list must carry DNS stubs only:\n%s", units[UnitMieru])
	}
	// Caddy stays unrestricted — its unit must keep loopback to reach the
	// panel backend; forward_proxy egress is governed by the ACL instead.
	if caddy, ok := units[UnitCaddy]; ok {
		if strings.Contains(caddy, "IPAddressDeny=") {
			t.Fatalf("veil-caddy must not carry the broad deny — it needs panel loopback:\n%s", caddy)
		}
	}
}

// #1097 follow-up: the deny list must never contain the blanket IPv4-mapped
// prefix ::ffff:0:0/96. Hysteria's ACL engine normalizes a plain IPv4
// destination into ::ffff: space before matching IPv6 prefixes, so that one
// entry rejects EVERY IPv4 egress (verified against pinned hysteria
// app/v2.12.2: any proxied IPv4 destination returned rep 0x04 "host
// unreachable"). The mapped forms of the denied IPv4 classes are listed
// instead, which still blocks ::ffff:<denied> smuggling.
func TestEgressDenyCIDRsUsesMappedFormsNotBlanketMappedPrefix(t *testing.T) {
	for _, cidr := range egressDenyCIDRs {
		if cidr == "::ffff:0:0/96" {
			t.Fatalf("blanket mapped prefix denies all IPv4 egress under hysteria-style normalization: %v", egressDenyCIDRs)
		}
	}
	// Every IPv4 deny entry must have its mapped-form counterpart so
	// ::ffff:<v4> literals cannot smuggle denied destinations.
	var v4 []string
	for _, cidr := range egressDenyCIDRs {
		if strings.Contains(cidr, ":") {
			continue
		}
		v4 = append(v4, cidr)
	}
	if len(v4) == 0 {
		t.Fatal("expected IPv4 entries in egressDenyCIDRs")
	}
	for _, cidr := range v4 {
		base, bits, _ := strings.Cut(cidr, "/")
		n, err := strconv.Atoi(bits)
		if err != nil {
			t.Fatalf("parse %s: %v", cidr, err)
		}
		want := "::ffff:" + base + "/" + strconv.Itoa(n+96)
		found := false
		for _, other := range egressDenyCIDRs {
			if other == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing mapped counterpart %s for %s in %v", want, cidr, egressDenyCIDRs)
		}
	}
}
