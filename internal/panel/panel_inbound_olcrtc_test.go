package panel

import (
	"strings"
	"testing"
)

// TestInboundOlcrtcFormUsesRealProvidersAndGenerateButton guards the olcRTC
// inbound form: real providers/transports, a room "Generate" button wired to
// the backend, and provider-aware enable/disable.
func TestInboundOlcrtcFormUsesRealProvidersAndGenerateButton(t *testing.T) {
	js := panelInboundActionsJS()
	for _, want := range []string{
		`<option value="jitsi" data-autoroom="true">jitsi</option>`,
		`<option value="telemost" data-autoroom="false">telemost</option>`,
		`<option value="wbstream" data-autoroom="false">wbstream</option>`,
		`<option value="vp8channel">vp8channel</option>`,
		`id="gen-olcrtc-room-btn"`,
		`window.updateOlcrtcGenerateButton =`,
		`/api/olcrtc/room`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("olcRTC inbound form missing %q", want)
		}
	}
	// The previous fake providers/transports must be gone.
	for _, gone := range []string{`livekit`, `value="external"`, `value="websocket"`, `value="quic"`} {
		if strings.Contains(js, gone) {
			t.Fatalf("olcRTC inbound form still references removed option %q", gone)
		}
	}
}
