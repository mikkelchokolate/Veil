package api

import "github.com/veil-panel/veil/internal/service"

type ServiceCommandPolicy struct{}

func NewServiceCommandPolicy() ServiceCommandPolicy { return ServiceCommandPolicy{} }

func (ServiceCommandPolicy) policy() service.CommandPolicy {
	return service.NewCommandPolicy(NewManagedRuntimeCatalog())
}

func (p ServiceCommandPolicy) AllowsAction(command []string) bool {
	return p.policy().AllowsAction(command)
}

func (p ServiceCommandPolicy) AllowsReload(command []string) bool {
	return p.policy().AllowsReload(command)
}

func (p ServiceCommandPolicy) AllowsHealth(serviceName string) bool {
	return p.policy().AllowsHealth(serviceName)
}

func runFixedServiceAction(command []string) ServiceActionResult {
	return service.RunFixedServiceAction(command, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}

func runFixedServiceHealthCheck(serviceName string) ServiceHealthResult {
	return service.RunFixedServiceHealthCheck(serviceName, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}

func isAllowedServiceCommand(command []string) bool {
	return NewServiceCommandPolicy().AllowsAction(command)
}

func isAllowedHealthService(serviceName string) bool {
	return NewServiceCommandPolicy().AllowsHealth(serviceName)
}
