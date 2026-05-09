package api

import "github.com/veil-panel/veil/internal/service"

type ServiceHealthCollection struct {
	inner service.ServiceHealthCollection
}

func NewServiceHealthCollection(check func(name string) ServiceHealthResult) ServiceHealthCollection {
	if check == nil {
		check = serviceHealthChecker
	}
	return ServiceHealthCollection{inner: service.NewServiceHealthCollection(func(name string) ServiceHealthResult {
		return check(name)
	})}
}

func (c ServiceHealthCollection) Check(actions []ServiceActionResult) []ServiceHealthResult {
	return c.inner.Check(actions)
}

func checkServiceHealth(actions []ServiceActionResult) []ServiceHealthResult {
	return NewServiceHealthCollection(serviceHealthChecker).Check(actions)
}
