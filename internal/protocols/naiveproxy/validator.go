package naiveproxy

import (
	"errors"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings ensures the global settings and credentials required by
// NaiveProxy/Caddy are present.
func (p Plugin) ValidateSettings(settings model.Settings, inbound model.Inbound) error {
	if strings.TrimSpace(settings.Domain) == "" || strings.TrimSpace(settings.Email) == "" {
		return errNaiveCaddySettingsRequired{}
	}
	if !p.HasCredential(settings, inbound) {
		return errors.New("naive username and password are required")
	}
	return nil
}

// ValidateInbound checks one inbound for naiveproxy-specific problems.
func (p Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	if p.HasCredential(settings, inbound) {
		return nil
	}
	return []model.ValidationIssue{{
		Code:      "naive_credential_required",
		Severity:  "error",
		Field:     "inbound",
		InboundID: inbound.Name,
		Message:   "NaiveProxy inbound requires a username and password",
	}}
}

// NeedsDomain reports that naiveproxy needs a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return true }

// NeedsEmail reports that naiveproxy needs an email for ACME TLS.
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool { return true }

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
