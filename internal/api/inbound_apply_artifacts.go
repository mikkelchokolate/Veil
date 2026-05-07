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
	return InboundApplyArtifacts{byProtocol: map[string]InboundApplyArtifact{
		"naiveproxy": {Config: "/etc/veil/generated/caddy/Caddyfile", Action: "reload veil-naive.service", ValidateInboundRender: true},
		"hysteria2":  {Config: "/etc/veil/generated/hysteria2/server.yaml", Action: "reload veil-hysteria2.service", ValidateInboundRender: true},
		"mieru":      {Config: "/etc/veil/generated/mieru/server_config.json", Action: "restart veil-mieru.service"},
	}}
}

func (a InboundApplyArtifacts) ForProtocol(protocol string) (InboundApplyArtifact, bool) {
	artifact, ok := a.byProtocol[protocol]
	return artifact, ok
}
