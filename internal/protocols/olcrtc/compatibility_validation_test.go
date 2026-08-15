package olcrtc

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidateInboundEnforcesPinnedUpstreamCompatibilityMatrix(t *testing.T) {
	key := strings.Repeat("a", 64)
	valid := []struct {
		auth      string
		transport string
	}{
		{"jitsi", "datachannel"},
		{"jitsi", "vp8channel"},
		{"jitsi", "seichannel"},
		{"jitsi", "videochannel"},
		{"telemost", "vp8channel"},
		{"telemost", "videochannel"},
		{"wbstream", "vp8channel"},
		{"wbstream", "seichannel"},
		{"wbstream", "videochannel"},
	}
	for _, tc := range valid {
		t.Run(tc.auth+"_"+tc.transport, func(t *testing.T) {
			room := "room-1"
			if tc.auth == "jitsi" {
				room = "https://meet.example.org/room-1"
			}
			issues := (Plugin{}).ValidateInbound(model.Settings{}, olcrtcValidationInbound(key, tc.auth, tc.transport, room))
			if hasOlcrtcIssue(issues, "olcrtc_provider_transport_unsupported") || hasOlcrtcIssue(issues, "olcrtc_wbstream_datachannel") {
				t.Fatalf("working upstream combination was rejected: %+v", issues)
			}
		})
	}

	invalid := []struct {
		auth      string
		transport string
		code      string
	}{
		{"telemost", "datachannel", "olcrtc_provider_transport_unsupported"},
		{"telemost", "seichannel", "olcrtc_provider_transport_unsupported"},
		{"wbstream", "datachannel", "olcrtc_wbstream_datachannel"},
	}
	for _, tc := range invalid {
		t.Run("reject_"+tc.auth+"_"+tc.transport, func(t *testing.T) {
			issues := (Plugin{}).ValidateInbound(model.Settings{}, olcrtcValidationInbound(key, tc.auth, tc.transport, "room-1"))
			issue, ok := olcrtcIssue(issues, tc.code)
			if !ok {
				t.Fatalf("expected %s, got %+v", tc.code, issues)
			}
			if issue.Severity != "error" {
				t.Fatalf("%s severity = %q, want error", tc.code, issue.Severity)
			}
		})
	}
}

func TestValidateInboundRejectsJitsiRoomWithoutHostAndPath(t *testing.T) {
	key := strings.Repeat("a", 64)
	for _, room := range []string{"room-only", "https://meet.example.org", "/room", "   "} {
		t.Run(strings.ReplaceAll(room, "/", "_"), func(t *testing.T) {
			issues := (Plugin{}).ValidateInbound(model.Settings{}, olcrtcValidationInbound(key, "jitsi", "datachannel", room))
			if room == "   " {
				if !hasOlcrtcIssue(issues, "olcrtc_jitsi_room_invalid") && !hasOlcrtcIssue(issues, "olcrtc_room_required") {
					t.Fatalf("expected room validation error, got %+v", issues)
				}
				return
			}
			if !hasOlcrtcIssue(issues, "olcrtc_jitsi_room_invalid") {
				t.Fatalf("expected olcrtc_jitsi_room_invalid, got %+v", issues)
			}
		})
	}
}

func TestValidateInboundAcceptsPinnedUpstreamJitsiRoomForms(t *testing.T) {
	key := strings.Repeat("a", 64)
	for _, room := range []string{
		"https://meet.example.org/room",
		"http://meet.example.org/room",
		"meet.example.org/room",
	} {
		issues := (Plugin{}).ValidateInbound(model.Settings{}, olcrtcValidationInbound(key, "jitsi", "datachannel", room))
		if hasOlcrtcIssue(issues, "olcrtc_jitsi_room_invalid") {
			t.Fatalf("room %q should match upstream parser: %+v", room, issues)
		}
	}
}

func TestUppercaseHexKeyIsPreserved(t *testing.T) {
	key := strings.Repeat("A", 64)
	if !isOlcrtcKey(key) {
		t.Fatal("uppercase hex is accepted by upstream hex.DecodeString and must be accepted by Veil")
	}
	out, err := (Plugin{}).Autofill(model.Inbound{
		Name:           "olc",
		Protocol:       "olcrtc",
		Password:       key,
		OlcrtcAuth:     "jitsi",
		OlcrtcRoomID:   "https://meet.example.org/room",
		ProtocolFields: map[string]any{"olcrtcAuth": "jitsi", "olcrtcRoomID": "https://meet.example.org/room"},
	})
	if err != nil {
		t.Fatalf("Autofill: %v", err)
	}
	if out.Password != key {
		t.Fatalf("Autofill replaced valid uppercase key: got %q want %q", out.Password, key)
	}
}

func olcrtcValidationInbound(key, auth, transport, room string) model.Inbound {
	return model.Inbound{
		Protocol:        "olcrtc",
		Password:        key,
		OlcrtcAuth:      auth,
		OlcrtcTransport: transport,
		OlcrtcRoomID:    room,
		ProtocolFields: map[string]any{
			"olcrtcAuth":      auth,
			"olcrtcTransport": transport,
			"olcrtcRoomID":    room,
		},
	}
}

func hasOlcrtcIssue(issues []model.ValidationIssue, code string) bool {
	_, ok := olcrtcIssue(issues, code)
	return ok
}

func olcrtcIssue(issues []model.ValidationIssue, code string) (model.ValidationIssue, bool) {
	for _, issue := range issues {
		if issue.Code == code {
			return issue, true
		}
	}
	return model.ValidationIssue{}, false
}
