package api

type ServiceRuntimeStatusReader func(unit string) ServiceRuntimeStatus

type ManagedServiceStatusCatalog struct {
	read ServiceRuntimeStatusReader
}

func NewManagedServiceStatusCatalog(read ServiceRuntimeStatusReader) ManagedServiceStatusCatalog {
	if read == nil {
		read = serviceStatusReader
	}
	return ManagedServiceStatusCatalog{read: read}
}

func (c ManagedServiceStatusCatalog) List() []ServiceStatus {
	runtimes := NewManagedRuntimeCatalog().Runtimes()
	services := make([]ServiceStatus, 0, len(runtimes))
	for _, runtime := range runtimes {
		services = append(services, ServiceStatus{Name: runtime.Name, Managed: true, Transport: runtime.Transport, Unit: runtime.Unit})
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

func buildServiceStatuses() []ServiceStatus {
	return NewManagedServiceStatusCatalog(serviceStatusReader).List()
}
