package api

type CaddyRequirement struct{}

func NewCaddyRequirement() CaddyRequirement { return CaddyRequirement{} }

func (CaddyRequirement) Required(settings Settings, inbounds []Inbound) bool {
	if settings.PanelAccess == "caddy" {
		return true
	}
	protocols := NewInboundProtocolCatalog()
	for _, inbound := range inbounds {
		if inbound.Enabled && protocols.RequiresCaddy(inbound.Protocol) {
			return true
		}
	}
	return false
}
