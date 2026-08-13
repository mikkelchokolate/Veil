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
			Severity:    "warning",
			Field:       "password",
			Message:     "olcRTC encryption key is empty and will be generated on apply",
			Remediation: "Set a key or leave empty to auto-generate.",
			Source:      "olcrtc",
		})
	}
	auth := olcrtcAuth(settings, inbound)
	room := olcrtcRoomID(settings, inbound)
	if room == "" {
		if ProviderSupportsAutoRoom(auth) {
			issues = append(issues, model.ValidationIssue{
				Code:        "olcrtc_room_missing",
				Severity:    "warning",
				Field:       "olcrtcRoomID",
				Message:     "olcRTC room is empty and will be generated on apply",
				Remediation: "Leave empty to auto-generate for " + auth + ".",
				Source:      "olcrtc",
			})
		} else {
			issues = append(issues, model.ValidationIssue{
				Code:        "olcrtc_room_required",
				Severity:    "error",
				Field:       "olcrtcRoomID",
				Message:     "olcRTC provider " + auth + " requires a room created on the service first",
				Remediation: "Create a room on " + auth + " and paste its id.",
				Source:      "olcrtc",
			})
		}
	}
	return issues
}

// NeedsDomain reports that olcRTC does not require a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return false }

// NeedsEmail reports that olcRTC does not need an email address.
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool { return false }

// HasCredential reports whether the inbound has a usable olcRTC credential.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	return strings.TrimSpace(inbound.Password) != ""
}
