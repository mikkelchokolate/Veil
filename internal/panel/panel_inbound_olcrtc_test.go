package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// TestInboundOlcrtcFormUsesRealProvidersAndGenerateButton guards the olcRTC
// inbound form: real providers/transports, a room "Generate" button wired to
// the backend through the protocol-agnostic room endpoint, and provider-aware
// enable/disable.
func TestInboundOlcrtcFormUsesRealProvidersAndGenerateButton(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`'/api/protocols/' + encodeURIComponent(protocol) + '/room'`,
		`veilRenderDynamicProtocolFields`,
		`veilGenerateProtocolField`,
		`generateActionField`,
		`updateRoomGenerateButton`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("olcRTC dynamic fields missing %q", want)
		}
	}
	// The renderer must no longer hardcode the olcRTC endpoint or field names.
	for _, gone := range []string{`/api/olcrtc/room`, `protocol-field-olcrtcAuth`, `updateOlcrtcGenerateButton`} {
		if strings.Contains(js, gone) {
			t.Fatalf("olcRTC dynamic fields still hardcode %q", gone)
		}
	}
	// The previous fake providers/transports must be gone from the generic renderer.
	for _, gone := range []string{`livekit`, `value="external"`, `value="websocket"`, `value="quic"`} {
		if strings.Contains(js, gone) {
			t.Fatalf("olcRTC dynamic fields still references removed option %q", gone)
		}
	}

	// The schema itself carries the real providers, the auto-room flag, and the
	// field that feeds the room generator.
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
	findField := func(key string) (schema.FieldSchema, bool) {
		for _, f := range olcrtc.InboundFieldSchema {
			if f.Key == key {
				return f, true
			}
		}
		return schema.FieldSchema{}, false
	}
	if _, ok := findField("olcrtcAuth"); !ok {
		t.Fatalf("olcRTC schema missing olcrtcAuth: %+v", olcrtc.InboundFieldSchema)
	}
	if _, ok := findField("olcrtcTransport"); !ok {
		t.Fatalf("olcRTC schema missing olcrtcTransport: %+v", olcrtc.InboundFieldSchema)
	}
	roomField, ok := findField("olcrtcRoomID")
	if !ok {
		t.Fatalf("olcRTC schema missing olcrtcRoomID: %+v", olcrtc.InboundFieldSchema)
	}
	if roomField.GenerateAction != "room" {
		t.Fatalf("olcrtcRoomID generateAction = %q, want room", roomField.GenerateAction)
	}
	if roomField.GenerateActionField != "olcrtcAuth" {
		t.Fatalf("olcrtcRoomID generateActionField = %q, want olcrtcAuth", roomField.GenerateActionField)
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
