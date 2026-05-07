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
	services := []ServiceStatus{
		{Name: "veil", Managed: true, Unit: "veil.service"},
		{Name: "naive", Managed: true, Transport: "tcp", Unit: "caddy.service"},
		{Name: "hysteria2", Managed: true, Transport: "udp", Unit: "hysteria2.service"},
		{Name: "sing-box", Managed: true, Unit: "sing-box.service"},
		{Name: "mieru", Managed: true, Unit: "mieru.service"},
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
