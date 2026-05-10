package api

import "github.com/veil-panel/veil/internal/service"

func checkServiceHealth(actions []ServiceActionResult) []ServiceHealthResult {
	return service.NewServiceHealthCollection(func(name string) ServiceHealthResult {
		return serviceHealthChecker(name)
	}).Check(actions)
}
