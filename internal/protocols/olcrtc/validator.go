package olcrtc

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for olcRTC global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound reports olcRTC-specific inbound issues that would otherwise
// render a config the pinned upstream runtime cannot use.
func (Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	key := olcrtcKey(inbound)
	if key != "" && !isOlcrtcKey(key) {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_key_invalid",
			Severity:    "error",
			Field:       "password",
			Message:     "olcRTC encryption key must be 64 hex characters",
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
	authValid := isOneOf(auth, "jitsi", "telemost", "wbstream")
	transportValid := isOneOf(transport, "datachannel", "vp8channel", "seichannel", "videochannel")
	if !authValid {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_auth_invalid",
			Severity:    "error",
			Field:       "olcrtcAuth",
			Message:     "olcRTC auth provider must be one of jitsi, telemost, wbstream",
			Remediation: "Pick a supported provider.",
			Source:      "olcrtc",
		})
	}
	if !transportValid {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_transport_invalid",
			Severity:    "error",
			Field:       "olcrtcTransport",
			Message:     "olcRTC transport must be one of datachannel, vp8channel, seichannel, videochannel",
			Remediation: "Pick a supported transport.",
			Source:      "olcrtc",
		})
	}

	// Match the E2E compatibility matrix of the exact pinned olcRTC runtime.
	// Telemost removed DataChannel and does not support SEI. WBStream guest
	// tokens cannot publish DataChannel; making it work requires auth.token
	// with publish/moderator rights, which Veil does not expose today.
	if authValid && transportValid {
		switch {
		case auth == "telemost" && (transport == "datachannel" || transport == "seichannel"):
			issues = append(issues, model.ValidationIssue{
				Code:        "olcrtc_provider_transport_unsupported",
				Severity:    "error",
				Field:       "olcrtcTransport",
				Message:     "the selected olcRTC transport is not supported by Telemost",
				Remediation: "Use vp8channel or videochannel with Telemost.",
				Source:      "olcrtc",
			})
		case auth == "wbstream" && transport == "datachannel":
			issues = append(issues, model.ValidationIssue{
				Code:        "olcrtc_wbstream_datachannel",
				Severity:    "error",
				Field:       "olcrtcTransport",
				Message:     "wbstream datachannel requires an auth token with publish rights, which Veil does not expose",
				Remediation: "Use vp8channel, seichannel, or videochannel with wbstream.",
				Source:      "olcrtc",
			})
		}
	}

	room := olcrtcRoomID(settings, inbound)
	if room == "" {
		// No provider gets a room auto-created at apply time: GenerateRoom
		// runs only in Autofill and the /api/protocols/{protocol}/room
		// endpoint. A rendered empty room.id makes the daemon exit with
		// ErrRoomIDRequired, so this must be an error.
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_room_required",
			Severity:    "error",
			Field:       "olcrtcRoomID",
			Message:     "olcRTC room is empty",
			Remediation: "Generate a room in the panel (the Generate button) and save it; no room is created at apply time.",
			Source:      "olcrtc",
		})
	} else if auth == "jitsi" && !validJitsiRoom(room) {
		issues = append(issues, model.ValidationIssue{
			Code:        "olcrtc_jitsi_room_invalid",
			Severity:    "error",
			Field:       "olcrtcRoomID",
			Message:     "Jitsi rooms must include both a host and room path",
			Remediation: "Use a room such as https://meet.example.org/room-name or generate one in the panel.",
			Source:      "olcrtc",
		})
	}
	return issues
}

// validJitsiRoom mirrors the pinned upstream provider parser. The runtime
// accepts a scheme-prefixed URL or host/room, then requires a non-empty host
// and a non-empty room path. Keeping this check aligned prevents apply from
// publishing a config that immediately exits in the olcRTC process.
func validJitsiRoom(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if idx := strings.Index(raw, "://"); idx >= 0 {
		raw = raw[idx+3:]
	}
	raw = strings.TrimPrefix(raw, "//")
	raw = strings.TrimPrefix(raw, "/")
	slash := strings.Index(raw, "/")
	if slash <= 0 {
		return false
	}
	host := strings.TrimSpace(raw[:slash])
	room := strings.Trim(raw[slash+1:], "/")
	return host != "" && room != ""
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
func (Plugin) HasCredential(_ model.Settings, inbound model.Inbound) bool {
	return olcrtcKey(inbound) != ""
}
