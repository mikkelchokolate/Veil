package naiveproxy

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// Validator is an alias for Plugin so inbound validation can be invoked with a
// dedicated receiver type while keeping the existing plugin interface intact.
type Validator = Plugin

// ValidateSettings is a no-op for naiveproxy. Per-inbound domain, email, and
// credential validation are handled by ValidateInbound and the apply-plan builder.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error {
	return nil
}

// ValidateInbound checks one inbound for naiveproxy-specific problems.
func (p Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	domain := NaiveDomain(settings, inbound)
	if domain == "" {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_domain_required",
			Severity: "error",
			Field:    "domain",
			Message:  "Naive inbound requires a public domain.",
			Source:   "naiveproxy",
		})
	} else if err := hostenv.ValidateDomain(domain); err != nil {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_domain_invalid",
			Severity: "error",
			Field:    "domain",
			Message:  "Naive inbound domain is invalid: " + err.Error(),
			Source:   "naiveproxy",
		})
	}
	if email := model.ResolveInboundEmail(inbound, settings); email == "" {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_email_required",
			Severity: "error",
			Field:    "email",
			Message:  "Naive inbound requires an ACME email.",
			Source:   "naiveproxy",
		})
	} else if err := hostenv.ValidateEmail(email); err != nil {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_email_invalid",
			Severity: "error",
			Field:    "email",
			Message:  "Naive inbound email is invalid: " + err.Error(),
			Source:   "naiveproxy",
		})
	}
	transport := NaiveTransport(inbound)
	switch transport {
	case "tcp":
	default:
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_transport_invalid",
			Severity: "error",
			Field:    "transport",
			Message:  "NaiveProxy supports only the tcp transport in this release",
			Source:   "naiveproxy",
		})
	}
	port := NaivePublicPort(settings, inbound)
	if port < 1 || port > 65535 {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_public_port_invalid",
			Severity: "error",
			Field:    "publicPort",
			Message:  "publicPort must be between 1 and 65535",
			Source:   "naiveproxy",
		})
	}
	// credential presence preserved from existing validator
	if !p.HasCredential(settings, inbound) {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_credential_required",
			Severity: "error",
			Field:    "profiles",
			Message:  "At least one username/password profile is required.",
			Source:   "naiveproxy",
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
