package api

type StatusResponseBuilder struct {
	info     ServerInfo
	services func() []ServiceStatus
}

func NewStatusResponseBuilder(info ServerInfo, services func() []ServiceStatus) StatusResponseBuilder {
	if services == nil {
		services = buildServiceStatuses
	}
	return StatusResponseBuilder{info: info, services: services}
}

func (b StatusResponseBuilder) Build() StatusResponse {
	return StatusResponse{
		SchemaVersion: "v1",
		Name:          "Veil",
		Version:       b.info.Version,
		Mode:          b.info.Mode,
		Services:      b.services(),
	}
}
