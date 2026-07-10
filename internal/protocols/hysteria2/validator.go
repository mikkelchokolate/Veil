package hysteria2

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for Hysteria2 global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound is a no-op for Hysteria2-specific inbound checks.
func (Plugin) ValidateInbound(model.Settings, model.Inbound) []model.ValidationIssue { return nil }

// NeedsDomain reports that Hysteria2 needs a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return true }

// NeedsEmail reports that Hysteria2 does not need an email address.
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool { return false }

// HasCredential reports whether the inbound has a usable Hysteria2 credential.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if profile.Enabled && strings.TrimSpace(profile.Password) != "" {
			return true
		}
	}
	password := hysteria2Password(settings, inbound)
	return password != ""
}
