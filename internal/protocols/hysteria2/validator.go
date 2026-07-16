package hysteria2

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for Hysteria2 global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound checks the Hysteria2 inbound for a valid public domain and
// ACME email when a per-inbound domain is configured.
func (Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	domain := model.ResolveInboundDomain(inbound, settings)
	if domain == "" {
		issues = append(issues, model.ValidationIssue{
			Code:     "hysteria2_domain_required",
			Severity: "error",
			Field:    "domain",
			Message:  "Hysteria2 inbound requires a public domain.",
			Source:   "hysteria2",
		})
	} else if err := hostenv.ValidateDomain(domain); err != nil {
		issues = append(issues, model.ValidationIssue{
			Code:     "hysteria2_domain_invalid",
			Severity: "error",
			Field:    "domain",
			Message:  "Hysteria2 inbound domain is invalid: " + err.Error(),
			Source:   "hysteria2",
		})
	}
	if model.InboundDomain(inbound) != "" {
		if email := model.ResolveInboundEmail(inbound, settings); email == "" {
			issues = append(issues, model.ValidationIssue{
				Code:     "hysteria2_email_required",
				Severity: "error",
				Field:    "email",
				Message:  "Hysteria2 inbound with a custom domain requires an ACME email.",
				Source:   "hysteria2",
			})
		} else if err := hostenv.ValidateEmail(email); err != nil {
			issues = append(issues, model.ValidationIssue{
				Code:     "hysteria2_email_invalid",
				Severity: "error",
				Field:    "email",
				Message:  "Hysteria2 inbound email is invalid: " + err.Error(),
				Source:   "hysteria2",
			})
		}
	}
	return issues
}

// NeedsDomain reports that Hysteria2 needs a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return true }

// NeedsEmail reports that Hysteria2 needs an email address when it uses a
// per-inbound domain with Caddy-managed TLS.
func (Plugin) NeedsEmail(_ model.Settings, inbound model.Inbound) bool {
	return model.InboundDomain(inbound) != ""
}

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
