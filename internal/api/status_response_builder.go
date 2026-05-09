package api

import "github.com/veil-panel/veil/internal/service"

type StatusResponseBuilder struct {
	inner service.StatusResponseBuilder
}

func NewStatusResponseBuilder(info ServerInfo, services func() []ServiceStatus) StatusResponseBuilder {
	if services == nil {
		services = buildServiceStatuses
	}
	return StatusResponseBuilder{inner: service.NewStatusResponseBuilder(service.StatusInfo{Version: info.Version, Mode: info.Mode}, services)}
}

func (b StatusResponseBuilder) Build() StatusResponse {
	return b.inner.Build()
}
