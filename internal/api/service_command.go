package api

import "github.com/veil-panel/veil/internal/service"

func runFixedServiceAction(command []string) ServiceActionResult {
	return service.RunFixedServiceAction(command, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}

func runFixedServiceHealthCheck(serviceName string) ServiceHealthResult {
	return service.RunFixedServiceHealthCheck(serviceName, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}
