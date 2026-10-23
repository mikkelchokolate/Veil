package olcrtc

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestRenderConfigUsesResolverDNSNotServerDomain guards the olcRTC fix from
// #823 on the production plugin path: net.dns must be a DNS resolver, never
// our server domain, and the WebRTC transport must be a channel type (default
// datachannel), never the inbound's L4 transport ("udp"). It used to run only
// against the dead generatedconfig.RenderOlcrtcInbound helper (#1273 item 2).
func TestRenderConfigUsesResolverDNSNotServerDomain(t *testing.T) {
	key := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	inbound := model.Inbound{
		Name: "olc", Protocol: "olcrtc", Transport: "udp", Port: 6523, Enabled: true,
		OlcrtcAuth: "jitsi", OlcrtcRoomID: "https://meet.handyweb.org/veil-abcd1234",
		Password: key,
	}
	arts, ok, err := New().RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "45.157.233.54"},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: []model.Inbound{inbound},
	})
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !ok || len(arts) != 1 {
		t.Fatalf("RenderConfig ok=%v artifacts=%d, want one artifact", ok, len(arts))
	}
	out := arts[0].Body
	if strings.Contains(out, "45.157.233.54") {
		t.Fatalf("olcrtc config must not put the server domain in dns:\n%s", out)
	}
	// Positive golden: the resolver must be present with the product default;
	// omitting net.dns entirely would still pass the absence check above.
	for _, want := range []string{
		"dns: 1.1.1.1:53",
		"transport: datachannel",
		"provider: jitsi",
		"https://meet.handyweb.org/veil-abcd1234",
		key,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("olcrtc config missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "transport: udp") {
		t.Fatalf("olcrtc must not use the L4 transport udp:\n%s", out)
	}
}
