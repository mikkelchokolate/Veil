package api

type ApplyProtocolCapability struct {
	Protocol               string
	Config                 string
	Action                 string
	ValidateInboundRender  bool
	RequiresRenderSettings bool
	RequiresCaddySettings  bool
}

type ApplyProtocolCapabilityCatalog struct {
	byProtocol map[string]ApplyProtocolCapability
}

func NewApplyProtocolCapabilityCatalog() ApplyProtocolCapabilityCatalog {
	runtimes := NewManagedRuntimeCatalog()
	naiveAction, _ := runtimes.ApplyAction("naiveproxy")
	hysteriaAction, _ := runtimes.ApplyAction("hysteria2")
	mieruAction, _ := runtimes.ApplyAction("mieru")
	return ApplyProtocolCapabilityCatalog{byProtocol: map[string]ApplyProtocolCapability{
		"naiveproxy": {Protocol: "naiveproxy", Config: "/etc/veil/generated/caddy/Caddyfile", Action: naiveAction, ValidateInboundRender: true, RequiresRenderSettings: true, RequiresCaddySettings: true},
		"hysteria2":  {Protocol: "hysteria2", Config: "/etc/veil/generated/hysteria2/server.yaml", Action: hysteriaAction, ValidateInboundRender: true, RequiresRenderSettings: true},
		"mieru":      {Protocol: "mieru", Config: "/etc/veil/generated/mieru/server_config.json", Action: mieruAction, ValidateInboundRender: true},
	}}
}

func (c ApplyProtocolCapabilityCatalog) ForProtocol(protocol string) (ApplyProtocolCapability, bool) {
	capability, ok := c.byProtocol[protocol]
	return capability, ok
}

func (c ApplyProtocolCapability) ValidateSettings(settings Settings) error {
	if c.RequiresCaddySettings {
		return NewNaiveCaddySettingsRequirement().Validate(settings)
	}
	return nil
}

func (c ApplyProtocolCapability) ShouldValidateRender(renderSettingsAvailable bool) bool {
	return c.ValidateInboundRender && (!c.RequiresRenderSettings || renderSettingsAvailable)
}
