package olcrtc

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for olcRTC global settings.
func (Plugin) ValidateSettings(model.Settings) error { return nil }

// ValidateInbound is a no-op for olcRTC-specific inbound checks.
func (Plugin) ValidateInbound(model.Settings, model.Inbound) []model.ValidationIssue { return nil }

// NeedsDomain reports that olcRTC does not require a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return false }

// HasCredential reports whether the inbound has a usable olcRTC credential.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	return strings.TrimSpace(inbound.Password) != ""
}
