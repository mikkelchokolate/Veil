package naiveproxy

import (
	"errors"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings ensures the global settings and per-inbound credentials
// required by NaiveProxy/Caddy are present.
func (Plugin) ValidateSettings(settings model.Settings, inbound model.Inbound) error {
	if strings.TrimSpace(settings.Domain) == "" || strings.TrimSpace(settings.Email) == "" {
		return errNaiveCaddySettingsRequired{}
	}
	username := naiveUsername(settings, inbound)
	password := naivePassword(settings, inbound)
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return errors.New("naive username and password are required")
	}
	return nil
}

// ValidateInbound checks one inbound for naiveproxy-specific problems.
func (Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	username := naiveUsername(settings, inbound)
	password := naivePassword(settings, inbound)
	if username == "" || password == "" {
		issues = append(issues, model.ValidationIssue{
			Code:      "naive_credential_required",
			Severity:  "error",
			Field:     "inbound",
			InboundID: inbound.Name,
			Message:   "NaiveProxy inbound requires a username and password",
		})
	}
	return issues
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
