package panelaccess

import (
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
