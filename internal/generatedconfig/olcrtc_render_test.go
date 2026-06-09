package generatedconfig

import (
	"strings"
	"testing"
)

// TestRenderOlcrtcUsesResolverDNSNotServerDomain guards the fix for olcRTC
// configs: net.dns must be a DNS resolver, never our server domain, and the
// WebRTC transport must be a channel type (default datachannel), never the
// inbound's L4 transport ("udp").
func TestRenderOlcrtcUsesResolverDNSNotServerDomain(t *testing.T) {
	settings := Settings{Domain: "45.157.233.54"}
	inbound := Inbound{
		Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523,
		OlcrtcAuth: "jitsi", OlcrtcRoomID: "https://meet.small-dm.ru/veil-abcd1234",
		Password: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	out, err := RenderOlcrtcInbound(settings, inbound, WarpConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "45.157.233.54") {
		t.Fatalf("olcrtc config must not put the server domain in dns:\n%s", out)
	}
	if !strings.Contains(out, "transport: datachannel") {
		t.Fatalf("olcrtc transport should default to datachannel, got:\n%s", out)
	}
	if strings.Contains(out, "transport: udp") {
		t.Fatalf("olcrtc must not use the L4 transport udp:\n%s", out)
	}
	if !strings.Contains(out, "provider: jitsi") {
		t.Fatalf("olcrtc provider missing:\n%s", out)
	}
	if !strings.Contains(out, "https://meet.small-dm.ru/veil-abcd1234") {
		t.Fatalf("olcrtc room id missing:\n%s", out)
	}
	if !strings.Contains(out, inbound.Password) {
		t.Fatalf("olcrtc crypto key (inbound password) missing:\n%s", out)
	}
}
