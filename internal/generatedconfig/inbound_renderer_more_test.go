package generatedconfig

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestRenderNaiveInboundRendersAndReturnsPaths(t *testing.T) {
	settings := Settings{
		Domain:        "vpn.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "veil",
		NaivePassword: "global-secret",
	}
	body, err := RenderNaiveInbound(settings, Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}, WarpConfig{}, false)
	if err != nil {
		t.Fatalf("RenderNaiveInbound: %v", err)
	}
	// Exact site-address + credential goldens — a soft-OR accepts a body
	// missing either half (#857).
	if !strings.Contains(body, ":443, vpn.example.com {") {
		t.Fatalf("missing exact site address:\n%s", body)
	}
	if !strings.Contains(body, "basic_auth veil global-secret") {
		t.Fatalf("missing naive credential:\n%s", body)
	}

	r := NewInboundRenderer(settings, NewPaths("/etc/veil"), WarpConfig{})
	if got := r.Paths().ApplyRoot; got != "/etc/veil" {
		t.Fatalf("Paths().ApplyRoot = %q", got)
	}
	// The name promises returned paths — pin the consolidated caddy config
	// path consumers write to, not just the root (#857).
	if got := r.Paths().CaddyJSON(); got != "/etc/veil/generated/caddy/config.json" {
		t.Fatalf("Paths().CaddyJSON() = %q", got)
	}
}

func TestRenderHysteria2InboundRenders(t *testing.T) {
	settings := Settings{Domain: "vpn.example.com", Hysteria2Password: "global"}
	body, err := RenderHysteria2Inbound(settings, Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true}, WarpConfig{})
	if err != nil {
		t.Fatalf("RenderHysteria2Inbound: %v", err)
	}
	// A listen-only check greens a body that drops auth entirely — pin the
	// password mode and the settings credential (#857).
	for _, want := range []string{"listen: :8443", "type: password", "password: global"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered hysteria2 config missing %q:\n%s", want, body)
		}
	}
}

func TestInboundRendererNaiveUsesFallbackPasswordsAndDefaultWarpPort(t *testing.T) {
	settings := Settings{
		Domain:        "vpn.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "veil",
		NaivePassword: "global-secret",
	}
	inbound := Inbound{
		Name:          "naive",
		Protocol:      "naiveproxy",
		Transport:     "tcp",
		Port:          443,
		Enabled:       true,
		NaivePassword: "inbound-secret",
	}
	renderer := NewInboundRenderer(settings, NewPaths("/etc/veil"), WarpConfig{Enabled: true})
	body, err := renderer.RenderNaive(inbound, false)
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	if !strings.Contains(body, "inbound-secret") {
		t.Fatalf("expected inbound naive password:\n%s", body)
	}
	if !strings.Contains(body, "upstream socks5://127.0.0.1:40000") {
		t.Fatalf("expected default warp socks port:\n%s", body)
	}
}

func TestInboundRendererNaiveReturnsClientAccessError(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderNaive(Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		Profiles:  []ClientProfile{{Name: "alice", Enabled: true}},
	}, false)
	if err == nil {
		t.Fatal("expected client access error for profile missing password")
	}
}

func TestInboundRendererNaiveReturnsRendererError(t *testing.T) {
	renderer := NewInboundRenderer(Settings{}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderNaive(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}, false)
	if err == nil {
		t.Fatal("expected renderer error for missing domain")
	}
}

func TestInboundRendererHysteria2UsesFallbackPasswordsAndDefaultWarpPort(t *testing.T) {
	settings := Settings{Domain: "vpn.example.com", Hysteria2Password: "global-secret"}
	inbound := Inbound{
		Name:              "hy2",
		Protocol:          "hysteria2",
		Transport:         "udp",
		Port:              8443,
		Enabled:           true,
		Hysteria2Password: "inbound-secret",
	}
	renderer := NewInboundRenderer(settings, NewPaths("/etc/veil"), WarpConfig{Enabled: true})
	body, err := renderer.RenderHysteria2(inbound)
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	if !strings.Contains(body, "inbound-secret") {
		t.Fatalf("expected inbound hysteria2 password:\n%s", body)
	}
	if !strings.Contains(body, "addr: 127.0.0.1:40000") {
		t.Fatalf("expected default warp upstream port:\n%s", body)
	}
}

func TestInboundRendererHysteria2ReturnsClientAccessError(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderHysteria2(Inbound{
		Name:      "hy2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Enabled:   true,
		Profiles:  []ClientProfile{{Name: "alice", Enabled: true}},
	})
	if err == nil {
		t.Fatal("expected client access error for profile missing password")
	}
}

func TestInboundRendererHysteria2ReturnsRendererError(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderHysteria2(Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 0, Enabled: true})
	if err == nil {
		t.Fatal("expected renderer error for port 0")
	}
}

func TestInboundRendererOlcrtcGeneratesRandomPassword(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderOlcrtc(Inbound{Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523, Enabled: true})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	// Presence of "key:" greens an empty or non-hex key — the product emits
	// 64 lowercase hex chars (32 random bytes), so pin the value (#857).
	m := regexp.MustCompile(`(?m)^  key: ([0-9a-f]{64})$`)
	if !m.MatchString(body) {
		t.Fatalf("generated key is not 64 hex chars:\n%s", body)
	}
}

func TestInboundRendererOlcrtcReturnsRandomError(t *testing.T) {
	old := randRead
	randRead = func(b []byte) (int, error) {
		return 0, errors.New("random source exhausted")
	}
	t.Cleanup(func() { randRead = old })

	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderOlcrtc(Inbound{Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523, Enabled: true})
	if err == nil || err.Error() != "random source exhausted" {
		t.Fatalf("expected random read error, got %v", err)
	}
}

func TestInboundRendererOlcrtcUsesSettingDefaults(t *testing.T) {
	settings := Settings{
		Domain:          "vpn.example.com",
		OlcrtcAuth:      "custom",
		OlcrtcRoomID:    "https://room.example.com/abc",
		OlcrtcTransport: "vp8channel",
	}
	renderer := NewInboundRenderer(settings, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderOlcrtc(Inbound{Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523, Enabled: true, Password: "secret"})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "provider: custom") {
		t.Fatalf("expected auth from settings:\n%s", body)
	}
	if !strings.Contains(body, "https://room.example.com/abc") {
		t.Fatalf("expected room id from settings:\n%s", body)
	}
	if !strings.Contains(body, "transport: vp8channel") {
		t.Fatalf("expected transport from settings:\n%s", body)
	}
}

func TestInboundRendererOlcrtcDefaultsAuthToJitsi(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderOlcrtc(Inbound{Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523, Enabled: true, Password: "secret"})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "provider: jitsi") {
		t.Fatalf("expected default jitsi auth:\n%s", body)
	}
}

func TestRenderPanelStandaloneSkipsNonCaddyAccess(t *testing.T) {
	renderer := NewInboundRenderer(Settings{PanelAccess: "none"}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderPanelStandalone()
	if err != nil || body != "" {
		t.Fatalf("expected empty body and nil error, got %q %v", body, err)
	}
}

func TestRenderPanelStandaloneRequiresWebBasePath(t *testing.T) {
	renderer := NewInboundRenderer(Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderPanelStandalone()
	if err == nil || !strings.Contains(err.Error(), "webBasePath is required") {
		t.Fatalf("expected webBasePath error, got %v", err)
	}
}

func TestRenderPanelStandaloneRequiresHostPort(t *testing.T) {
	renderer := NewInboundRenderer(Settings{PanelAccess: "caddy", PanelListen: "2096", WebBasePath: "/panel/"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderPanelStandalone()
	if err == nil || !strings.Contains(err.Error(), "panelListen must be host:port") {
		t.Fatalf("expected host:port error, got %v", err)
	}
}

func TestRenderPanelStandaloneRejectsInvalidPort(t *testing.T) {
	renderer := NewInboundRenderer(Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:99999", WebBasePath: "/panel/"}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderPanelStandalone()
	if err == nil || !strings.Contains(err.Error(), "panelListen must be host:port") {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestRenderPanelStandaloneRenders(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:2096",
		WebBasePath: "/panel/",
	}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderPanelStandalone()
	if err != nil {
		t.Fatalf("RenderPanelStandalone: %v", err)
	}
	if !strings.Contains(body, "panel.example.com") || !strings.Contains(body, "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("unexpected panel body:\n%s", body)
	}
}

func TestRenderPanelStandaloneReturnsRendererError(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:2096",
		WebBasePath: "/panel/",
	}, NewPaths("/etc/veil"), WarpConfig{})
	_, err := renderer.RenderPanelStandalone()
	if err == nil || !strings.Contains(err.Error(), "domain is required") {
		t.Fatalf("expected renderer error, got %v", err)
	}
}

func TestPanelCaddyRouteBranches(t *testing.T) {
	if _, ok, err := panelCaddyRoute(Settings{PanelAccess: "none"}); err != nil || ok {
		t.Fatalf("non-caddy access should return false: ok=%v err=%v", ok, err)
	}
	if _, ok, err := panelCaddyRoute(Settings{PanelAccess: "caddy", WebBasePath: "/panel/", PanelListen: "bad"}); err == nil || ok {
		t.Fatalf("expected host:port error: ok=%v err=%v", ok, err)
	}
	if _, ok, err := panelCaddyRoute(Settings{PanelAccess: "caddy", WebBasePath: "/panel/", PanelListen: "127.0.0.1:0"}); err == nil || ok {
		t.Fatalf("expected invalid port error: ok=%v err=%v", ok, err)
	}
}
