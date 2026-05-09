package api

import "github.com/veil-panel/veil/internal/service"

type ServiceHealthPolicy = service.HealthPolicy

func NewServiceHealthPolicy() ServiceHealthPolicy { return service.NewHealthPolicy() }

func requireHealthyServices(checks []ServiceHealthResult) error {
	return service.NewHealthPolicy().RequireHealthy(checks)
}
