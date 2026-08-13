package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestSanitizeInboundProtocolFieldsDropsUnknownKeys locks in audit #48/#98:
// protocolFields keys not declared by the selected protocol's inbound schema
// must be dropped before persistence so stale cross-protocol fields or raw-API
// junk never reach renderers/links.
func TestSanitizeInboundProtocolFieldsDropsUnknownKeys(t *testing.T) {
	fields := map[string]any{
		// Declared for hysteria2.
		"hysteria2Password": "pw",
		"hysteria2Insecure": true,
		"masqueradeURL":     "https://example.com",
		// Stale keys from other protocols / junk must be dropped.
		"olcrtcAuth":     "jitsi",
		"olcrtcRoomID":   "room-1",
		"naivePassword":  "npw",
		"password":       "legacy",
		"arbitraryJunk":  42,
		"does.not.exist": "x",
		"__proto__":      "polluted",
		"constructor":    map[string]any{"x": 1},
	}
	clean := sanitizeInboundProtocolFields("hysteria2", fields)
	if len(clean) != 3 {
		t.Fatalf("clean = %+v, want exactly the 3 hysteria2-declared keys", clean)
	}
	for _, key := range []string{"hysteria2Password", "hysteria2Insecure", "masqueradeURL"} {
		if _, ok := clean[key]; !ok {
			t.Fatalf("declared key %q was dropped: %+v", key, clean)
		}
	}
}

// TestSanitizeInboundProtocolFieldsKeepsMieruPassword ensures the mieru
// dynamic password field survives (its schema declares "password").
func TestSanitizeInboundProtocolFieldsKeepsMieruPassword(t *testing.T) {
	fields := map[string]any{"password": "secret", "stale": true}
	clean := sanitizeInboundProtocolFields("mieru", fields)
	if clean["password"] != "secret" {
		t.Fatalf("mieru password dropped: %+v", clean)
	}
	if _, ok := clean["stale"]; ok {
		t.Fatalf("stale key survived: %+v", clean)
	}
}

// TestAutofillInboundDropsStaleCrossProtocolFields is the end-to-end guard:
// switching an inbound from olcrtc to hysteria2 must not carry olcrtc keys
// into the stored state.
func TestAutofillInboundDropsStaleCrossProtocolFields(t *testing.T) {
	filled, err := autofillInbound(model.Inbound{
		Name:     "edge",
		Protocol: "hysteria2",
		Port:     443,
		Enabled:  true,
		ProtocolFields: map[string]any{
			"olcrtcAuth":   "jitsi",
			"olcrtcRoomID": "room-1",
			"password":     "stale-olcrtc-key",
		},
	})
	if err != nil {
		t.Fatalf("autofill: %v", err)
	}
	if _, ok := filled.ProtocolFields["olcrtcAuth"]; ok {
		t.Fatalf("stale olcrtcAuth survived on hysteria2 inbound: %+v", filled.ProtocolFields)
	}
	if _, ok := filled.ProtocolFields["olcrtcRoomID"]; ok {
		t.Fatalf("stale olcrtcRoomID survived on hysteria2 inbound: %+v", filled.ProtocolFields)
	}
}
