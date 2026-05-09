package api

import "github.com/veil-panel/veil/internal/panelaccess"

type CaddyRequirement struct {
	inner panelaccess.CaddyRequirement
}

func NewCaddyRequirement() CaddyRequirement {
	return CaddyRequirement{inner: panelaccess.NewCaddyRequirement(NewInboundProtocolCatalog().RequiresCaddy)}
}

func (r CaddyRequirement) Required(settings Settings, inbounds []Inbound) bool {
	return r.inner.Required(settings, inbounds)
}
