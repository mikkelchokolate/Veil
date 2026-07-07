package service

type StatusInfo struct {
	Version string
	Mode    string
}

type StatusResponse struct {
	SchemaVersion string          `json:"schemaVersion"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Mode          string          `json:"mode"`
	Services      []ServiceStatus `json:"services"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	ActionName  string `json:"actionName,omitempty"`
	Managed     bool   `json:"managed"`
	Restartable bool   `json:"restartable,omitempty"`
	Transport   string `json:"transport,omitempty"`
	Unit        string `json:"unit,omitempty"`
	LoadState   string `json:"loadState,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	Error       string `json:"error,omitempty"`
}

type StatusResponseBuilder struct {
	info     StatusInfo
	services func() []ServiceStatus
}

func NewStatusResponseBuilder(info StatusInfo, services func() []ServiceStatus) StatusResponseBuilder {
	if services == nil {
		services = func() []ServiceStatus { return nil }
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

type RuntimeStatusReader func(unit string) RuntimeStatus

type ManagedServiceStatusCatalog struct {
	catalog ManagedRuntimeCatalog
	read    RuntimeStatusReader
}

func NewManagedServiceStatusCatalog(catalog ManagedRuntimeCatalog, read RuntimeStatusReader) ManagedServiceStatusCatalog {
	if read == nil {
		read = ReadSystemdServiceStatus
	}
	return ManagedServiceStatusCatalog{catalog: catalog, read: read}
}

func (c ManagedServiceStatusCatalog) List() []ServiceStatus {
	runtimes := c.catalog.Runtimes()
	services := make([]ServiceStatus, 0, len(runtimes))
	for _, runtime := range runtimes {
		services = append(services, ServiceStatus{
			Name: runtime.Name, ActionName: runtime.ActionName,
			Managed: true, Restartable: runtime.ManualRestart,
			Transport: runtime.Transport, Unit: runtime.Unit,
		})
	}
	for i := range services {
		runtime := c.read(services[i].Unit)
		services[i].LoadState = runtime.LoadState
		services[i].ActiveState = runtime.ActiveState
		services[i].SubState = runtime.SubState
		services[i].Error = runtime.Error
	}
	return services
}
