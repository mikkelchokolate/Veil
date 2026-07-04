package naiveproxy

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings ensures the global settings needed by naiveproxy are present.
func (Plugin) ValidateSettings(settings model.Settings) error {
	username := protocolString(settings.ProtocolFields, "naiveUsername", settings.NaiveUsername)
	password := protocolString(settings.ProtocolFields, "naivePassword", settings.NaivePassword)
	if strings.TrimSpace(settings.Domain) == "" || strings.TrimSpace(settings.Email) == "" || strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return errNaiveCaddySettingsRequired{}
	}
	return nil
}

// ValidateInbound checks one inbound for naiveproxy-specific problems.
func (Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	return nil
}

// NeedsDomain reports that naiveproxy needs a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return true }

// HasCredential reports whether the inbound has a usable naiveproxy credential.
func (p Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if !profile.Enabled || strings.TrimSpace(profile.Password) == "" {
			continue
		}
		if strings.TrimSpace(profile.Username) != "" {
			return true
		}
	}
	username := naiveUsername(settings, inbound)
	password := naivePassword(settings, inbound)
	return username != "" && password != ""
}

type errNaiveCaddySettingsRequired struct{}

func (errNaiveCaddySettingsRequired) Error() string {
	return "domain, email, naive username, and naive password are required for NaiveProxy/Caddy"
}
