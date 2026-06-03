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

func (NaiveCaddySettingsRequirement) Validate(settings model.Settings) error {
	if strings.TrimSpace(settings.Domain) == "" || strings.TrimSpace(settings.Email) == "" || strings.TrimSpace(settings.NaiveUsername) == "" || strings.TrimSpace(settings.NaivePassword) == "" {
		return errNaiveCaddySettingsRequired{}
	}
	return nil
}

type errNaiveCaddySettingsRequired struct{}

func (errNaiveCaddySettingsRequired) Error() string {
	return "domain, email, naive username, and naive password are required for NaiveProxy/Caddy"
}
