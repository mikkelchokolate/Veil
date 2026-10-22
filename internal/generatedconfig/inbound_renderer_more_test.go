package generatedconfig

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestInboundRendererReturnsPaths(t *testing.T) {
	settings := Settings{Domain: "vpn.example.com", Email: "admin@example.com"}
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
	if !strings.Contains(body, "addr: 127.41.0.1:40000") {
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
