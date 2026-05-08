package api

type InboundApplyArtifact = ApplyProtocolCapability

type InboundApplyArtifacts struct {
	catalog ApplyProtocolCapabilityCatalog
}

func NewInboundApplyArtifacts() InboundApplyArtifacts {
	return InboundApplyArtifacts{catalog: NewApplyProtocolCapabilityCatalog()}
}

func (a InboundApplyArtifacts) ForProtocol(protocol string) (InboundApplyArtifact, bool) {
	return a.catalog.ForProtocol(protocol)
}
