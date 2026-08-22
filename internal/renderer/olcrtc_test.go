package renderer

import (
	"strings"
	"testing"
)

func TestRenderOlcrtcProducesValidServerYAML(t *testing.T) {
	body, err := RenderOlcrtc(OlcrtcConfig{
		Auth:      "jitsi",
		RoomID:    "https://meet.example.com/myroom",
		Key:       "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Transport: "datachannel",
		DNS:       "1.1.1.1:53",
	})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	for _, want := range []string{
		"mode: srv",
		"provider: jitsi",
		"id: https://meet.example.com/myroom",
		"key: abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		"transport: datachannel",
		"dns: 1.1.1.1:53",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("output missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data:") {
		t.Fatalf("default config must use olcRTC's embedded dictionaries:\n%s", body)
	}
}

func TestRenderOlcrtcKeepsExplicitDictionaryPath(t *testing.T) {
	body, err := RenderOlcrtc(OlcrtcConfig{
		Auth:   "jitsi",
		RoomID: "https://meet.example.com/myroom",
		Data:   "/var/lib/veil/olcrtc/custom",
		Key:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "data: /var/lib/veil/olcrtc/custom") {
		t.Fatalf("explicit dictionary path missing:\n%s", body)
	}
}

func TestRenderOlcrtcRequiresKey(t *testing.T) {
	_, err := RenderOlcrtc(OlcrtcConfig{
		Auth:      "jitsi",
		RoomID:    "https://meet.example.com/myroom",
		Transport: "datachannel",
	})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestRenderOlcrtcDefaultsDNS(t *testing.T) {
	body, err := RenderOlcrtc(OlcrtcConfig{
		Auth:      "jitsi",
		RoomID:    "https://meet.example.com/myroom",
		Key:       "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Transport: "datachannel",
	})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "dns: 1.1.1.1:53") {
		t.Fatalf("expected default DNS:\n%s", body)
	}
}

func TestRenderOlcrtcDefaultsTransport(t *testing.T) {
	body, err := RenderOlcrtc(OlcrtcConfig{
		Auth:   "jitsi",
		RoomID: "https://meet.example.com/myroom",
		Key:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	})
	if err != nil {
		t.Fatalf("RenderOlcrtc: %v", err)
	}
	if !strings.Contains(body, "transport: datachannel") {
		t.Fatalf("expected default transport:\n%s", body)
	}
}
