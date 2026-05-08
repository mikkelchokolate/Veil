package api

type InboundApplyArtifact struct {
	Config                string
	Action                string
	ValidateInboundRender bool
}

type InboundApplyArtifacts struct {
	byProtocol map[string]InboundApplyArtifact
}

func NewInboundApplyArtifacts() InboundApplyArtifacts {
	catalog := NewManagedRuntimeCatalog()
	naiveAction, _ := catalog.ApplyAction("naiveproxy")
	hysteriaAction, _ := catalog.ApplyAction("hysteria2")
	mieruAction, _ := catalog.ApplyAction("mieru")
	return InboundApplyArtifacts{byProtocol: map[string]InboundApplyArtifact{
		"naiveproxy": {Config: "/etc/veil/generated/caddy/Caddyfile", Action: naiveAction, ValidateInboundRender: true},
		"hysteria2":  {Config: "/etc/veil/generated/hysteria2/server.yaml", Action: hysteriaAction, ValidateInboundRender: true},
		"mieru":      {Config: "/etc/veil/generated/mieru/server_config.json", Action: mieruAction},
	}}
}

func (a InboundApplyArtifacts) ForProtocol(protocol string) (InboundApplyArtifact, bool) {
	artifact, ok := a.byProtocol[protocol]
	return artifact, ok
}
