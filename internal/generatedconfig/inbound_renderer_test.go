package generatedconfig

import (
	"strings"
	"testing"
)

func TestInboundRendererRendersNaiveWithClientProfilesAndPanelCaddyRoute(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain:        "panel.example.com",
		Email:         "admin@example.com",
		PanelAccess:   "caddy",
		PanelListen:   "127.0.0.1:2096",
		WebBasePath:   "/panel-secret/",
		NaiveUsername: "veil",
		NaivePassword: "global-secret",
		FallbackRoot:  "/var/lib/veil/www",
	}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderNaive(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true}}})
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	for _, want := range []string{"basic_auth alice alice-pass", "reverse_proxy 127.0.0.1:2096", "handle_path /panel-secret/*"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestInboundRendererRendersHysteria2WithPerInboundPassword(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com", Hysteria2Password: "global", MasqueradeURL: "https://www.bing.com/"}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderHysteria2(Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "per-inbound"})
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	for _, want := range []string{"listen: :8443", "password: per-inbound", "masquerade"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestInboundRendererRendersUpstreamSocksWhenWarpEnabled(t *testing.T) {
	warp := WarpConfig{Enabled: true, SocksPort: 40050}
	
	// Test Naive
	naiveRenderer := NewInboundRenderer(Settings{
		Domain:        "panel.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "veil",
		NaivePassword: "global-secret",
	}, NewPaths("/etc/veil"), warp)
	naiveBody, err := naiveRenderer.RenderNaive(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true})
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	if !strings.Contains(naiveBody, "upstream socks5://127.0.0.1:40050") {
		t.Fatalf("naive body missing SOCKS5 upstream:\n%s", naiveBody)
	}
	
	// Test Hysteria2
	hyRenderer := NewInboundRenderer(Settings{Domain: "vpn.example.com", Hysteria2Password: "global"}, NewPaths("/etc/veil"), warp)
	hyBody, err := hyRenderer.RenderHysteria2(Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "pass"})
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	if !strings.Contains(hyBody, "outbound:") || !strings.Contains(hyBody, "socks5:") || !strings.Contains(hyBody, "addr: 127.0.0.1:40050") {
		t.Fatalf("hysteria2 body missing SOCKS5 upstream:\n%s", hyBody)
	}
}
