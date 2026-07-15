package naiveproxy

import (
	"errors"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Validator is retained as an alias for callers that use the dedicated name.
type Validator = Plugin

func (p Plugin) ValidateSettings(settings model.Settings, inbound model.Inbound) error {
	if strings.TrimSpace(NaiveDomain(settings, inbound)) == "" {
		return errors.New("naive public domain is required")
	}
	email := strings.TrimSpace(NaiveEmail(settings, inbound))
	if email == "" {
		email = strings.TrimSpace(settings.DefaultAcmeEmail)
	}
	if email == "" {
		email = strings.TrimSpace(settings.PanelEmail)
	}
	if email == "" {
		email = strings.TrimSpace(settings.Email)
	}
	if email == "" {
		return errors.New("ACME email is required for naiveproxy")
	}
	if !p.HasCredential(settings, inbound) {
		return errors.New("naive username and password are required")
	}
	return nil
}

func (p Plugin) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	var issues []model.ValidationIssue
	if strings.TrimSpace(NaiveDomain(settings, inbound)) == "" {
		issues = append(issues, model.ValidationIssue{
			Code: "naive_domain_required", Severity: "error", Field: "domain",
			InboundID: inbound.Name, Message: "NaiveProxy inbound requires a public domain", Source: "naiveproxy",
		})
	}
	if transport := NaiveTransport(inbound); transport != "tcp" {
		issues = append(issues, model.ValidationIssue{
			Code: "naive_transport_invalid", Severity: "error", Field: "transport",
			InboundID: inbound.Name, Message: "NaiveProxy supports only the tcp transport in this release", Source: "naiveproxy",
		})
	}
	if port := NaivePublicPort(settings, inbound); port < 1 || port > 65535 {
		issues = append(issues, model.ValidationIssue{
			Code: "naive_public_port_invalid", Severity: "error", Field: "publicPort",
			InboundID: inbound.Name, Message: "publicPort must be between 1 and 65535", Source: "naiveproxy",
		})
	}
	if !p.HasCredential(settings, inbound) {
		issues = append(issues, model.ValidationIssue{
			Code: "naive_credential_required", Severity: "error", Field: "profiles",
			InboundID: inbound.Name, Message: "At least one username/password profile is required", Source: "naiveproxy",
		})
	}
	return issues
}

func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return true }
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool  { return true }

func (p Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if !profile.Enabled || strings.TrimSpace(profile.Password) == "" {
			continue
		}
		if strings.TrimSpace(profile.Username) != "" || strings.TrimSpace(profile.Name) != "" {
			return true
		}
	}
	return naiveUsername(settings, inbound) != "" && naivePassword(settings, inbound) != ""
}
