package mieru

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for Mieru global settings.
func (Plugin) ValidateSettings(model.Settings) error { return nil }

// ValidateInbound is a no-op for Mieru-specific inbound checks.
func (Plugin) ValidateInbound(model.Settings, model.Inbound) []model.ValidationIssue { return nil }

// NeedsDomain reports that Mieru does not require a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return false }

// HasCredential reports whether the inbound has a usable Mieru credential.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if profile.Enabled && strings.TrimSpace(profile.Password) != "" {
			return true
		}
	}
	password := strings.TrimSpace(inbound.Password)
	if password == "" {
		password = protocolString(inbound.ProtocolFields, "password", "")
	}
	return password != ""
}
