package renderer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderWarpSingBoxConfigRequiresPrivateKeyAddressAndPeer(t *testing.T) {
	_, err := RenderWarpSingBox(WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:2408"})
	if err == nil {
		t.Fatal("expected missing WARP fields to fail")
	}
	if !strings.Contains(err.Error(), "private key") {
		t.Fatalf("expected private key validation error, got %v", err)
	}
}

func TestRenderWarpSingBoxConfigWritesWireGuardEndpointAndLocalSocksInbound(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint:      "engage.cloudflareclient.com:2408",
		PrivateKey:    "warp-private-key",
		LocalAddress:  "172.16.0.2/32,2606:4700:110:8a36::/128",
		PeerPublicKey: "bmXOC+F1L2oi7pR9...",
		Reserved:      []int{1, 2, 3},
		SocksListen:   "127.0.0.1",
		SocksPort:     40000,
		MTU:           1280,
	})
	if err != nil {
		t.Fatalf("render warp sing-box: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("rendered config should be valid JSON: %v\n%s", err, body)
	}
	// sing-box >= 1.11 schema: WireGuard is an endpoint, not an outbound.
	for _, want := range []string{
		`"type": "socks"`,
		`"listen": "127.0.0.1"`,
		`"listen_port": 40000`,
		`"endpoints":`,
		`"type": "wireguard"`,
		`"tag": "warp"`,
		`"private_key": "warp-private-key"`,
		`"address":`,
		`"172.16.0.2/32"`,
		`"peers":`,
		`"public_key": "bmXOC+F1L2oi7pR9..."`,
		`"allowed_ips":`,
		`"reserved":`,
		`"mtu": 1280`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered WARP config missing %q:\n%s", want, body)
		}
	}
	// The removed/old fields must NOT appear.
	for _, forbidden := range []string{`"local_address"`, `"peer_public_key"`, `"server_port"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("rendered WARP config must not contain removed field %q:\n%s", forbidden, body)
		}
	}
	// WireGuard must not be an outbound anymore.
	outbounds, _ := parsed["outbounds"].([]any)
	for _, o := range outbounds {
		if m, ok := o.(map[string]any); ok && m["type"] == "wireguard" {
			t.Fatalf("wireguard must be an endpoint, not an outbound:\n%s", body)
		}
	}
}

func TestRenderWarpSingBoxConfigGeoipPrivateUsesIpIsPrivate(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000,
		RoutingRules: []WarpRoutingRule{{Match: "geoip:private", Outbound: "direct"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, `"ip_is_private": true`) {
		t.Fatalf("geoip:private must map to ip_is_private:\n%s", body)
	}
	if strings.Contains(body, `"geoip"`) {
		t.Fatalf("removed inline geoip field must not appear:\n%s", body)
	}
	if !strings.Contains(body, `"final": "warp"`) {
		t.Fatalf("default route must be the WARP endpoint:\n%s", body)
	}
}

func TestRenderWarpSingBoxConfigCountryRulesUseRuleSets(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000,
		RoutingRules: []WarpRoutingRule{
			{Match: "geoip:ru", Outbound: "direct"},
			{Match: "geosite:ru-blocked", Outbound: "warp"},
			{Match: "all", Outbound: "warp"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`"rule_set": "geoip-ru"`,
		`"rule_set": "geosite-ru-blocked"`,
		`SagerNet/sing-geoip/rule-set/geoip-ru.srs`,
		`SagerNet/sing-geosite/rule-set/geosite-ru-blocked.srs`,
		`"download_detour": "direct"`,
		`"final": "warp"`,
		`"outbound": "direct"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered WARP routing missing %q:\n%s", want, body)
		}
	}
	// The removed inline geoip/geosite rule fields must not appear.
	if strings.Contains(body, `"geoip":`) || strings.Contains(body, `"geosite":`) {
		t.Fatalf("removed inline geoip/geosite rule fields must not appear:\n%s", body)
	}
}

func TestRenderWarpSingBoxConfigDomainMatchType(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000,
		RoutingRules: []WarpRoutingRule{{Match: "example.com", Outbound: "direct"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, `"domain_suffix": "example.com"`) {
		t.Fatalf("expected domain suffix match rule, got:\n%s", body)
	}
}

func TestRenderWarpSingBoxConfigSkipsEmptyRuleFields(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000,
		RoutingRules: []WarpRoutingRule{
			{Match: "", Outbound: "direct"},
			{Match: "geoip:ru", Outbound: ""},
			{Match: "geosite:ru-blocked", Outbound: "warp"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, `"rule_set": "geosite-ru-blocked"`) {
		t.Fatalf("expected valid geosite rule to be included:\n%s", body)
	}
	if strings.Contains(body, "geoip-ru") {
		t.Fatalf("rule with empty outbound must be skipped:\n%s", body)
	}
	if strings.Contains(body, `"domain"`) {
		t.Fatalf("empty-match rule must be skipped:\n%s", body)
	}
}

func TestRenderWarpSingBoxConfigAlwaysRoutesThroughWarpByDefault(t *testing.T) {
	for _, rules := range [][]WarpRoutingRule{nil, {}} {
		body, err := RenderWarpSingBox(WarpSingBoxConfig{
			Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000,
			RoutingRules: rules,
		})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		// Even with no rules, traffic must default to the WARP endpoint.
		if !strings.Contains(body, `"final": "warp"`) {
			t.Fatalf("empty rules must still route through WARP:\n%s", body)
		}
	}
}

func TestRenderWarpSingBoxConfigParses3xUICommaSeparatedMatch(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000,
		RoutingRules: []WarpRoutingRule{{
			Match:    `geosite:category-gov-ru,regexp:.*\.ru$,regexp:.*\.su$`,
			Outbound: "direct",
		}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`"rule_set": "geosite-category-gov-ru"`,
		`SagerNet/sing-geosite/rule-set/geosite-category-gov-ru.srs`,
		`"domain_regex": ".*\\.ru$"`,
		`"domain_regex": ".*\\.su$"`,
		`"outbound": "direct"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("3x-ui match missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "regexp:") || strings.Contains(body, "category-gov-ru,regexp") {
		t.Fatalf("comma-separated match was not split:\n%s", body)
	}
}
