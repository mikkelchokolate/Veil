package olcrtc

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for olcRTC global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound reports olcRTC-specific inbound issues: the encryption key
// shape and the room requirement for providers that cannot auto-create rooms.
// A missing key is a warning (the renderer generates one on demand); a
// malformed key and a missing room for non-auto providers are errors that would
// otherwise render a broken config or a broken client link.
func (Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	key := protocolString(inbound.ProtocolFields, "password", inbound.Password)
	if key != "" && !isOlcrtcKey(key) {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_key_invalid",
			Severity:    "error",
			Field:       "password",
			Message:     "olcRTC encryption key must be 64 lowercase hex characters",
			Remediation: "Use the generate action to create a valid key.",
			Source:      "olcrtc",
		})
	} else if key == "" {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_key_missing",
			Severity:    "error",
			Field:       "password",
			Message:     "olcRTC encryption key is empty",
			Remediation: "Use the generate action to create a valid key; an empty key renders a fresh random key per render and breaks client links (audit #95/#140).",
			Source:      "olcrtc",
		})
	}
	auth := olcrtcAuth(settings, inbound)
	transport := olcrtcTransport(settings, inbound)
	if !isOneOf(auth, "jitsi", "telemost", "wbstream") {
		// Unknown provider values render into YAML as-is and make the daemon
		// fail; report explicitly instead of the room error (audit #135).
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_auth_invalid",
			Severity:    "error",
			Field:       "olcrtcAuth",
			Message:     "olcRTC auth provider must be one of jitsi, telemost, wbstream",
			Remediation: "Pick a supported provider.",
			Source:      "olcrtc",
		})
	}
	if !isOneOf(transport, "datachannel", "vp8channel", "seichannel", "videochannel") {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_transport_invalid",
			Severity:    "error",
			Field:       "olcrtcTransport",
			Message:     "olcRTC transport must be one of datachannel, vp8channel, seichannel, videochannel",
			Remediation: "Pick a supported transport.",
			Source:      "olcrtc",
		})
	}
	// Upstream matrix: datachannel over wbstream requires an auth token, which
	// Veil does not expose, so the tunnel would come up silent. Surface a
	// warning instead of advertising a broken combination (audit #84).
	if auth == "wbstream" && transport == "datachannel" {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_wbstream_datachannel",
			Severity:    "warning",
			Field:       "olcrtcTransport",
			Message:     "wbstream datachannel tunnels require an auth token that Veil does not expose; data may not flow",
			Remediation: "Use vp8channel/seichannel/videochannel with wbstream, or a different provider.",
			Source:      "olcrtc",
		})
	}
	room := olcrtcRoomID(settings, inbound)
	if room == "" {
		// No provider gets a room auto-created at apply time: GenerateRoom
		// runs only in Autofill and the /api/protocols/{protocol}/room
		// endpoint. A rendered empty room.id makes the daemon exit with
		// ErrRoomIDRequired, so this must be an error, not the historical
		// "will be generated on apply" warning (audit #83/#87/#131).
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_room_required",
			Severity:    "error",
			Field:       "olcrtcRoomID",
			Message:     "olcRTC room is empty",
			Remediation: "Generate a room in the panel (the Generate button) and save it; no room is created at apply time.",
			Source:      "olcrtc",
		})
	}
	return issues
}

// NeedsDomain reports that olcRTC does not require a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return false }

// isOneOf reports whether value equals any of the given candidates.
func isOneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

// NeedsEmail reports that olcRTC does not need an email address.
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool { return false }

// HasCredential reports whether the inbound has a usable olcRTC credential.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	return strings.TrimSpace(inbound.Password) != ""
}
