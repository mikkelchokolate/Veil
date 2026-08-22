package olcrtc

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func validKey() string { return strings.Repeat("a", 64) }

func issuesByCode(t *testing.T, issues []model.ValidationIssue, code string) *model.ValidationIssue {
	t.Helper()
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

// TestValidateInboundMissingKeyIsError locks in audit #95/#140: an empty key
// renders a fresh random key per render (never persisted), so client links
// and the server config diverge. Missing key is an error, not a warning.
func TestValidateInboundMissingKeyIsError(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{})
	if got := issuesByCode(t, issues, "olcrtc_key_missing"); got == nil {
		t.Fatalf("expected olcrtc_key_missing issue, got %+v", issues)
	} else if got.Severity != "error" {
		t.Errorf("olcrtc_key_missing severity = %q, want error", got.Severity)
	}
}

func TestValidateInboundMalformedKeyIsError(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password: "not-a-hex-key",
	})
	got := issuesByCode(t, issues, "olcrtc_key_invalid")
	if got == nil {
		t.Fatalf("expected olcrtc_key_invalid issue, got %+v", issues)
	}
	if got.Severity != "error" {
		t.Errorf("olcrtc_key_invalid severity = %q, want error", got.Severity)
	}
	if issuesByCode(t, issues, "olcrtc_key_missing") != nil {
		t.Error("malformed key must not also report missing")
	}
}

func TestValidateInboundValidKeyNoKeyIssues(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:     validKey(),
		OlcrtcRoomID: "https://meet.handyweb.org/room",
		OlcrtcAuth:   "jitsi",
	})
	if issuesByCode(t, issues, "olcrtc_key_invalid") != nil ||
		issuesByCode(t, issues, "olcrtc_key_missing") != nil {
		t.Errorf("valid key reported issues: %+v", issues)
	}
}

func TestValidateInboundRoomRequiredForNonAutoProvider(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:     validKey(),
		OlcrtcAuth:   "telemost",
		OlcrtcRoomID: "",
	})
	got := issuesByCode(t, issues, "olcrtc_room_required")
	if got == nil {
		t.Fatalf("expected olcrtc_room_required for telemost without room, got %+v", issues)
	}
	if got.Severity != "error" {
		t.Errorf("olcrtc_room_required severity = %q, want error", got.Severity)
	}
}

// TestValidateInboundRoomRequiredForAutoProviderToo locks in audit #83/#87:
// an empty room must be an error for EVERY provider — no room is created at
// apply time, and an empty room.id makes the daemon exit with ErrRoomIDRequired.
func TestValidateInboundRoomRequiredForAutoProviderToo(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:   validKey(),
		OlcrtcAuth: "jitsi",
	})
	got := issuesByCode(t, issues, "olcrtc_room_required")
	if got == nil {
		t.Fatalf("expected olcrtc_room_required for jitsi without room, got %+v", issues)
	}
	if got.Severity != "error" {
		t.Errorf("olcrtc_room_required severity = %q, want error", got.Severity)
	}
	if issuesByCode(t, issues, "olcrtc_room_missing") != nil {
		t.Errorf("olcrtc_room_missing must no longer be emitted: %+v", issues)
	}
}

func TestValidateInboundRoomPresentNoRoomIssues(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:     validKey(),
		OlcrtcAuth:   "telemost",
		OlcrtcRoomID: "https://telemost.example/room-123",
	})
	if issuesByCode(t, issues, "olcrtc_room_required") != nil ||
		issuesByCode(t, issues, "olcrtc_room_missing") != nil {
		t.Errorf("present room reported room issues: %+v", issues)
	}
}

func TestValidateInboundProtocolFieldsWinOverFlat(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password: "bad-flat-key",
		ProtocolFields: map[string]any{
			"password": validKey(),
		},
	})
	if issuesByCode(t, issues, "olcrtc_key_invalid") != nil {
		t.Errorf("ProtocolFields key should win over flat: %+v", issues)
	}
}

func TestValidateInboundRejectsUnknownAuthProvider(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:     validKey(),
		OlcrtcAuth:   "garbage",
		OlcrtcRoomID: "https://meet.example/room",
	})
	got := issuesByCode(t, issues, "olcrtc_auth_invalid")
	if got == nil {
		t.Fatalf("expected olcrtc_auth_invalid for unknown provider, got %+v", issues)
	}
	if got.Severity != "error" {
		t.Errorf("olcrtc_auth_invalid severity = %q, want error", got.Severity)
	}
}

func TestValidateInboundRejectsUnknownTransport(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:        validKey(),
		OlcrtcAuth:      "jitsi",
		OlcrtcRoomID:    "https://meet.example/room",
		OlcrtcTransport: "garbage",
	})
	got := issuesByCode(t, issues, "olcrtc_transport_invalid")
	if got == nil {
		t.Fatalf("expected olcrtc_transport_invalid for unknown transport, got %+v", issues)
	}
	if got.Severity != "error" {
		t.Errorf("olcrtc_transport_invalid severity = %q, want error", got.Severity)
	}
}

func TestValidateInboundRejectsWbstreamDatachannel(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:        validKey(),
		OlcrtcAuth:      "wbstream",
		OlcrtcRoomID:    "room-123",
		OlcrtcTransport: "datachannel",
	})
	got := issuesByCode(t, issues, "olcrtc_wbstream_datachannel")
	if got == nil {
		t.Fatalf("expected olcrtc_wbstream_datachannel error, got %+v", issues)
	}
	if got.Severity != "error" {
		t.Errorf("olcrtc_wbstream_datachannel severity = %q, want error", got.Severity)
	}
}

func TestValidateInboundAcceptsKnownAuthAndTransport(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:        validKey(),
		OlcrtcAuth:      "telemost",
		OlcrtcRoomID:    "https://telemost.example/room",
		OlcrtcTransport: "vp8channel",
	})
	if issuesByCode(t, issues, "olcrtc_auth_invalid") != nil || issuesByCode(t, issues, "olcrtc_transport_invalid") != nil {
		t.Errorf("known auth/transport reported invalid: %+v", issues)
	}
}
