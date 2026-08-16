package mieru

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for Mieru global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound checks Mieru constraints that are stricter than the common
// inbound model. Upstream mita only accepts server ports in 1025..65535.
func (Plugin) ValidateInbound(_ model.Settings, inbound model.Inbound) []model.ValidationIssue {
	if inbound.Port >= 1025 && inbound.Port <= 65535 {
		return nil
	}
	return []model.ValidationIssue{{
		Code:        "mieru_port_invalid",
		Severity:    "error",
		Field:       "port",
		Message:     "Mieru server port must be between 1025 and 65535.",
		Remediation: "Choose a non-privileged Mieru TCP/UDP port from 1025 to 65535.",
		Source:      "mieru",
	}}
}

// NeedsDomain reports that Mieru does not require a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return false }

// NeedsEmail reports that Mieru does not need an email address.
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool { return false }

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
