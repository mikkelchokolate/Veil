package api

type ServiceHealthCollection struct {
	check func(name string) ServiceHealthResult
}

func NewServiceHealthCollection(check func(name string) ServiceHealthResult) ServiceHealthCollection {
	if check == nil {
		check = serviceHealthChecker
	}
	return ServiceHealthCollection{check: check}
}

func (c ServiceHealthCollection) Check(actions []ServiceActionResult) []ServiceHealthResult {
	checks := []ServiceHealthResult{}
	for _, action := range actions {
		if !action.Success || action.Name == "" {
			continue
		}
		checks = append(checks, c.check(action.Name))
	}
	return checks
}

func checkServiceHealth(actions []ServiceActionResult) []ServiceHealthResult {
	return NewServiceHealthCollection(serviceHealthChecker).Check(actions)
}
