package api

type CaddyRequirement struct{}

func NewCaddyRequirement() CaddyRequirement { return CaddyRequirement{} }

func (CaddyRequirement) Required(settings Settings, inbounds []Inbound) bool {
	if settings.PanelAccess == "caddy" {
		return true
	}
	for _, inbound := range inbounds {
		if inbound.Enabled && inbound.Protocol == "naiveproxy" {
			return true
		}
	}
	return false
}
