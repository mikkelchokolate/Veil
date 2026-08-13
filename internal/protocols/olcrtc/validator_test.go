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

func TestValidateInboundMissingKeyIsWarning(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{})
	if got := issuesByCode(t, issues, "olcrtc_key_missing"); got == nil {
		t.Fatalf("expected olcrtc_key_missing issue, got %+v", issues)
	} else if got.Severity != "warning" {
		t.Errorf("olcrtc_key_missing severity = %q, want warning", got.Severity)
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

func TestValidateInboundRoomMissingForAutoProviderIsWarning(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{
		Password:   validKey(),
		OlcrtcAuth: "jitsi",
	})
	got := issuesByCode(t, issues, "olcrtc_room_missing")
	if got == nil {
		t.Fatalf("expected olcrtc_room_missing for jitsi without room, got %+v", issues)
	}
	if got.Severity != "warning" {
		t.Errorf("olcrtc_room_missing severity = %q, want warning", got.Severity)
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
