package panelaccess

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type CaddyRequirement struct {
	requiresCaddy RequiresCaddyFunc
}

func NewCaddyRequirement(requiresCaddy RequiresCaddyFunc) CaddyRequirement {
	return CaddyRequirement{requiresCaddy: requiresCaddy}
}

func protocolString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(s)
}

func (r CaddyRequirement) Required(settings model.Settings, inbounds []model.Inbound) bool {
	if settings.PanelAccess == "caddy" {
		return true
	}
	for _, inbound := range inbounds {
		if inbound.Enabled && r.protocolRequiresCaddy(inbound.Protocol) {
			return true
		}
	}
	return false
}

func (r CaddyRequirement) protocolRequiresCaddy(protocol string) bool {
	return r.requiresCaddy != nil && r.requiresCaddy(protocol)
}

type NaiveCaddySettingsRequirement struct{}

func NewNaiveCaddySettingsRequirement() NaiveCaddySettingsRequirement {
	return NaiveCaddySettingsRequirement{}
}

func hasAcmeEmail(settings model.Settings) bool {
	return strings.TrimSpace(settings.Email) != "" || strings.TrimSpace(settings.DefaultAcmeEmail) != "" || strings.TrimSpace(settings.PanelEmail) != ""
}

func (NaiveCaddySettingsRequirement) Validate(settings model.Settings) error {
	username := protocolString(settings.ProtocolFields, "naiveUsername", settings.NaiveUsername)
	password := protocolString(settings.ProtocolFields, "naivePassword", settings.NaivePassword)
	if strings.TrimSpace(settings.Domain) == "" || !hasAcmeEmail(settings) || strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return errNaiveCaddySettingsRequired{}
	}
	return nil
}

type errNaiveCaddySettingsRequired struct{}

func (errNaiveCaddySettingsRequired) Error() string {
	return "domain, email, naive username, and naive password are required for NaiveProxy/Caddy"
}
