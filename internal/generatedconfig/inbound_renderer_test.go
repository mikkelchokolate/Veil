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
	body, err := renderer.RenderNaive(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true}}}, true)
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	for _, want := range []string{"basic_auth alice alice-pass", "reverse_proxy 127.0.0.1:2096", "handle /panel-secret/*"} {
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

func TestInboundRendererRendersHysteria2WithManagedCertWhenCaddyAccess(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain:      "vpn.example.com",
		PanelAccess: "caddy",
		Email:       "admin@example.com",
	}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderHysteria2(Inbound{
		Name:           "hy2",
		Protocol:       "hysteria2",
		Transport:      "udp",
		Port:           8443,
		Enabled:        true,
		Password:       "pass",
		ProtocolFields: map[string]any{"domain": "vpn.example.com"},
	})
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	for _, want := range []string{
		"cert: /etc/veil/certs/vpn.example.com.crt",
		"key: /etc/veil/certs/vpn.example.com.key",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestInboundRendererRendersHysteria2WithSelfSignedCertByDefault(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	body, err := renderer.RenderHysteria2(Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "pass"})
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	for _, want := range []string{
		"cert: /etc/veil/panel/tls.crt",
		"key: /etc/veil/panel/tls.key",
	} {
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
	naiveBody, err := naiveRenderer.RenderNaive(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}, false)
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
	if !strings.Contains(hyBody, "outbounds:") || !strings.Contains(hyBody, "socks5:") || !strings.Contains(hyBody, "addr: 127.0.0.1:40050") {
		t.Fatalf("hysteria2 body missing SOCKS5 upstream:\n%s", hyBody)
	}
	if strings.Contains(hyBody, "\noutbound:\n") {
		t.Fatalf("hysteria2 body contains singular outbound block:\n%s", hyBody)
	}
}

func TestInboundRendererNaiveReadsProtocolFields(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain:        "vpn.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "global-user",
		NaivePassword: "global-secret",
		FallbackRoot:  "/var/lib/veil/global",
	}, NewPaths("/etc/veil"), WarpConfig{})
	inbound := Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		ProtocolFields: map[string]any{
			"naiveUsername": "fields-user",
			"naivePassword": "fields-secret",
			"fallbackRoot":  "/var/lib/veil/fields",
		},
	}
	body, err := renderer.RenderNaive(inbound, false)
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	if !strings.Contains(body, "basic_auth fields-user fields-secret") {
		t.Fatalf("expected ProtocolFields credentials, got:\n%s", body)
	}
	if !strings.Contains(body, "root * /var/lib/veil/fields") {
		t.Fatalf("expected ProtocolFields fallback root, got:\n%s", body)
	}
}

func TestInboundRendererNaivePasswordPrecedence(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com", Email: "admin@example.com", NaiveUsername: "veil"}, NewPaths("/etc/veil"), WarpConfig{})

	// inbound.Password wins over ProtocolFields.
	inbound := Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		Password:  "password-field",
		ProtocolFields: map[string]any{
			"naivePassword": "protocol-fields",
		},
		NaivePassword: "legacy-field",
	}
	body, err := renderer.RenderNaive(inbound, false)
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	if !strings.Contains(body, "basic_auth veil password-field") {
		t.Fatalf("expected inbound.Password to win, got:\n%s", body)
	}

	// ProtocolFields wins over legacy flat field when Password is empty.
	inbound.Password = ""
	body, err = renderer.RenderNaive(inbound, false)
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	if !strings.Contains(body, "basic_auth veil protocol-fields") {
		t.Fatalf("expected ProtocolFields password to win, got:\n%s", body)
	}
}

func TestInboundRendererHysteria2ReadsProtocolFields(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain:            "vpn.example.com",
		Hysteria2Password: "global-secret",
		MasqueradeURL:     "https://www.bing.com/",
	}, NewPaths("/etc/veil"), WarpConfig{})
	inbound := Inbound{
		Name:      "hy2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Enabled:   true,
		ProtocolFields: map[string]any{
			"hysteria2Password": "fields-secret",
			"masqueradeURL":     "https://example.com/",
		},
	}
	body, err := renderer.RenderHysteria2(inbound)
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	if !strings.Contains(body, "password: fields-secret") {
		t.Fatalf("expected ProtocolFields password, got:\n%s", body)
	}
	if !strings.Contains(body, "url: https://example.com/") {
		t.Fatalf("expected ProtocolFields masquerade URL, got:\n%s", body)
	}
}

func TestInboundRendererHysteria2PasswordPrecedence(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})

	// inbound.Password wins over ProtocolFields.
	inbound := Inbound{
		Name:              "hy2",
		Protocol:          "hysteria2",
		Transport:         "udp",
		Port:              8443,
		Enabled:           true,
		Password:          "password-field",
		ProtocolFields:    map[string]any{"hysteria2Password": "protocol-fields"},
		Hysteria2Password: "legacy-field",
	}
	body, err := renderer.RenderHysteria2(inbound)
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	if !strings.Contains(body, "password: password-field") {
		t.Fatalf("expected inbound.Password to win, got:\n%s", body)
	}

	// ProtocolFields wins over legacy flat field when Password is empty.
	inbound.Password = ""
	body, err = renderer.RenderHysteria2(inbound)
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	if !strings.Contains(body, "password: protocol-fields") {
		t.Fatalf("expected ProtocolFields password to win, got:\n%s", body)
	}
}

func TestInboundRendererOlcrtcReadsProtocolFields(t *testing.T) {
	renderer := NewInboundRenderer(Settings{Domain: "vpn.example.com"}, NewPaths("/etc/veil"), WarpConfig{})
	inbound := Inbound{
		Name:      "olc",
		Protocol:  "olcrtc",
		Transport: "udp",
		Port:      6523,
		Enabled:   true,
		Password:  "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "custom",
			"olcrtcRoomID":    "https://room.example.com/abc",
			"olcrtcTransport": "vp8channel",
		},
	}
	body, err := renderer.RenderOlcrtc(inbound)
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "provider: custom") {
		t.Fatalf("expected ProtocolFields auth, got:\n%s", body)
	}
	if !strings.Contains(body, "https://room.example.com/abc") {
		t.Fatalf("expected ProtocolFields room id, got:\n%s", body)
	}
	if !strings.Contains(body, "transport: vp8channel") {
		t.Fatalf("expected ProtocolFields transport, got:\n%s", body)
	}
}

func TestInboundRendererOlcrtcProtocolFieldsPrecedence(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain:          "vpn.example.com",
		OlcrtcAuth:      "settings-auth",
		OlcrtcRoomID:    "settings-room",
		OlcrtcTransport: "settings-transport",
	}, NewPaths("/etc/veil"), WarpConfig{})
	inbound := Inbound{
		Name:      "olc",
		Protocol:  "olcrtc",
		Transport: "udp",
		Port:      6523,
		Enabled:   true,
		Password:  "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "inbound-fields-auth",
			"olcrtcRoomID":    "inbound-fields-room",
			"olcrtcTransport": "inbound-fields-transport",
		},
		OlcrtcAuth:      "inbound-legacy-auth",
		OlcrtcRoomID:    "inbound-legacy-room",
		OlcrtcTransport: "inbound-legacy-transport",
	}
	body, err := renderer.RenderOlcrtc(inbound)
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "provider: inbound-fields-auth") {
		t.Fatalf("expected inbound ProtocolFields auth to win, got:\n%s", body)
	}
	if !strings.Contains(body, "inbound-fields-room") {
		t.Fatalf("expected inbound ProtocolFields room id to win, got:\n%s", body)
	}
	if !strings.Contains(body, "transport: inbound-fields-transport") {
		t.Fatalf("expected inbound ProtocolFields transport to win, got:\n%s", body)
	}
}

func TestInboundRendererReadsSettingsProtocolFields(t *testing.T) {
	renderer := NewInboundRenderer(Settings{
		Domain: "vpn.example.com",
		ProtocolFields: map[string]any{
			"naiveUsername":     "settings-user",
			"naivePassword":     "settings-secret",
			"fallbackRoot":      "/var/lib/veil/settings",
			"hysteria2Password": "settings-hy-secret",
			"masqueradeURL":     "https://settings.example.com/",
			"olcrtcAuth":        "settings-auth",
			"olcrtcRoomID":      "settings-room",
			"olcrtcTransport":   "settings-transport",
		},
	}, NewPaths("/etc/veil"), WarpConfig{})

	naiveBody, err := renderer.RenderNaive(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}, false)
	if err != nil {
		t.Fatalf("RenderNaive: %v", err)
	}
	if !strings.Contains(naiveBody, "basic_auth settings-user settings-secret") {
		t.Fatalf("expected settings ProtocolFields naive credentials, got:\n%s", naiveBody)
	}
	if !strings.Contains(naiveBody, "root * /var/lib/veil/settings") {
		t.Fatalf("expected settings ProtocolFields fallback root, got:\n%s", naiveBody)
	}

	hyBody, err := renderer.RenderHysteria2(Inbound{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true})
	if err != nil {
		t.Fatalf("RenderHysteria2: %v", err)
	}
	if !strings.Contains(hyBody, "password: settings-hy-secret") {
		t.Fatalf("expected settings ProtocolFields hysteria2 password, got:\n%s", hyBody)
	}
	if !strings.Contains(hyBody, "url: https://settings.example.com/") {
		t.Fatalf("expected settings ProtocolFields masquerade URL, got:\n%s", hyBody)
	}

	olcBody, err := renderer.RenderOlcrtc(Inbound{Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523, Enabled: true})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(olcBody, "provider: settings-auth") {
		t.Fatalf("expected settings ProtocolFields olcrtc auth, got:\n%s", olcBody)
	}
	if !strings.Contains(olcBody, "settings-room") {
		t.Fatalf("expected settings ProtocolFields olcrtc room id, got:\n%s", olcBody)
	}
	if !strings.Contains(olcBody, "transport: settings-transport") {
		t.Fatalf("expected settings ProtocolFields olcrtc transport, got:\n%s", olcBody)
	}
}
