package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols"
)

// TestInboundOlcrtcFormUsesRealProvidersAndGenerateButton guards the olcRTC
// inbound form: real providers/transports, a room "Generate" button wired to
// the backend, and provider-aware enable/disable.
func TestInboundOlcrtcFormUsesRealProvidersAndGenerateButton(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`/api/olcrtc/room`,
		`veilRenderDynamicProtocolFields`,
		`veilGenerateProtocolField`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("olcRTC dynamic fields missing %q", want)
		}
	}
	// The previous fake providers/transports must be gone from the generic renderer.
	for _, gone := range []string{`livekit`, `value="external"`, `value="websocket"`, `value="quic"`} {
		if strings.Contains(js, gone) {
			t.Fatalf("olcRTC dynamic fields still references removed option %q", gone)
		}
	}

	// The schema itself carries the real providers and the auto-room flag.
	var olcrtc *protocols.ProtocolInfo
	for _, info := range protocols.NewRegistry().ProtocolInfos() {
		if info.Protocol == "olcrtc" {
			olcrtc = &info
			break
		}
	}
	if olcrtc == nil {
		t.Fatal("olcRTC plugin not registered")
	}
	findField := func(key string) bool {
		for _, f := range olcrtc.InboundFieldSchema {
			if f.Key == key {
				return true
			}
		}
		return false
	}
	if !findField("olcrtcAuth") || !findField("olcrtcTransport") || !findField("olcrtcRoomID") {
		t.Fatalf("olcRTC schema missing fields: %+v", olcrtc.InboundFieldSchema)
	}
	for _, provider := range []string{"jitsi", "telemost", "wbstream"} {
		found := false
		for _, f := range olcrtc.InboundFieldSchema {
			if f.Key == "olcrtcAuth" {
				for _, opt := range f.Options {
					if opt.Value == provider {
						found = true
						break
					}
				}
			}
		}
		if !found {
			t.Fatalf("olcRTC auth provider %q missing from schema", provider)
		}
	}
}
