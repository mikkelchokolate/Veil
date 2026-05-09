package api

import "github.com/veil-panel/veil/internal/service"

type ServiceRuntimeStatusReader func(unit string) ServiceRuntimeStatus

type ManagedServiceStatusCatalog struct {
	inner service.ManagedServiceStatusCatalog
}

func NewManagedServiceStatusCatalog(read ServiceRuntimeStatusReader) ManagedServiceStatusCatalog {
	if read == nil {
		read = serviceStatusReader
	}
	return ManagedServiceStatusCatalog{inner: service.NewManagedServiceStatusCatalog(NewManagedRuntimeCatalog(), service.RuntimeStatusReader(read))}
}

func (c ManagedServiceStatusCatalog) List() []ServiceStatus {
	return c.inner.List()
}

func buildServiceStatuses() []ServiceStatus {
	return NewManagedServiceStatusCatalog(serviceStatusReader).List()
}
