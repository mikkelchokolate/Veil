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

func TestRenderWarpSingBoxConfigWritesWireGuardOutboundAndLocalSocksInbound(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		Endpoint:      "engage.cloudflareclient.com:2408",
		PrivateKey:    "warp-private-key",
		LocalAddress:  "172.16.0.2/32",
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
	for _, want := range []string{
		`"type": "socks"`,
		`"listen": "127.0.0.1"`,
		`"listen_port": 40000`,
		`"type": "wireguard"`,
		`"tag": "warp"`,
		`"server": "engage.cloudflareclient.com"`,
		`"server_port": 2408`,
		`"private_key": "warp-private-key"`,
		`"local_address":`,
		`"172.16.0.2/32"`,
		`"peer_public_key": "bmXOC+F1L2oi7pR9..."`,
		`"reserved":`,
		`"mtu": 1280`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered WARP config missing %q:\n%s", want, body)
		}
	}
}
