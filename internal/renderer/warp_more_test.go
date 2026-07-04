package renderer

import (
	"strings"
	"testing"
)

func TestRenderWarpSingBoxValidation(t *testing.T) {
	tests := []struct {
		name      string
		cfg       WarpSingBoxConfig
		wantError string
	}{
		{
			name:      "missing local address",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", PeerPublicKey: "p"},
			wantError: "WARP local address is required",
		},
		{
			name:      "missing peer public key",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32"},
			wantError: "WARP peer public key is required",
		},
		{
			name:      "socks port too low",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: -1},
			wantError: "WARP SOCKS port must be between 1 and 65535",
		},
		{
			name:      "socks port too high",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 70000},
			wantError: "WARP SOCKS port must be between 1 and 65535",
		},
		{
			name:      "endpoint missing port",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000},
			wantError: "WARP endpoint must be host:port",
		},
		{
			name:      "endpoint invalid port",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:abc", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000},
			wantError: "WARP endpoint port must be between 1 and 65535",
		},
		{
			name:      "endpoint port out of range",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:0", PrivateKey: "k", LocalAddress: "172.16.0.2/32", PeerPublicKey: "p", SocksPort: 40000},
			wantError: "WARP endpoint port must be between 1 and 65535",
		},
		{
			name:      "local address empty after split",
			cfg:       WarpSingBoxConfig{Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "k", LocalAddress: ", ,", PeerPublicKey: "p", SocksPort: 40000},
			wantError: "WARP local address is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderWarpSingBox(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected %q, got: %v", tt.wantError, err)
			}
		})
	}
}

func TestRenderWarpSingBoxDefaults(t *testing.T) {
	body, err := RenderWarpSingBox(WarpSingBoxConfig{
		PrivateKey:    "k",
		LocalAddress:  "172.16.0.2/32",
		PeerPublicKey: "p",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		`"address": "engage.cloudflareclient.com"`,
		`"port": 2408`,
		`"listen": "127.0.0.1"`,
		`"listen_port": 40000`,
		`"mtu": 1280`,
		`"final": "warp"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in config:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"reserved"`) {
		t.Fatal("reserved must not be present when empty")
	}
}
