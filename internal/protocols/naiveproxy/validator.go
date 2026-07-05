package naiveproxy

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Validator is an alias for Plugin so inbound validation can be invoked with a
// dedicated receiver type while keeping the existing plugin interface intact.
type Validator = Plugin

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
func (p Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	domain := NaiveDomain(inbound)
	if domain == "" {
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_domain_required",
			Severity: "error",
			Field:    "domain",
			Message:  "Naive inbound requires a public domain.",
			Source:   "naiveproxy",
		})
	}
	transport := NaiveTransport(inbound)
	switch transport {
	case "tcp", "quic", "dual":
	default:
		issues = append(issues, model.ValidationIssue{
			Code:     "naive_transport_invalid",
			Severity: "error",
			Field:    "transport",
			Message:  "transport must be tcp, quic, or dual",
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
