package api

type ServiceCommandPolicy struct{}

func (p ServiceCommandPolicy) AllowsAction(command []string) bool {
	return NewManagedRuntimeCatalog().AllowsPromotedAction(command)
}

func (p ServiceCommandPolicy) AllowsReload(command []string) bool {
	return p.AllowsAction(command)
}

func (ServiceCommandPolicy) AllowsHealth(service string) bool {
	return NewManagedRuntimeCatalog().AllowsHealthUnit(service)
}

func runFixedServiceAction(command []string) ServiceActionResult {
	result := ServiceActionResult{Command: append([]string(nil), command...)}
	if len(command) > 0 {
		result.Name = command[len(command)-1]
	}
	if !isAllowedServiceCommand(command) {
		result.Error = "service command is not allowed"
		return result
	}
	output := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{Command: command})
	result.Output = output.Output
	if output.NotFound {
		result.Error = command[0] + " not found"
		return result
	}
	if output.TimedOut {
		result.Error = "service action timed out"
		return result
	}
	if output.Err != nil {
		result.Error = output.Err.Error()
		return result
	}
	result.Success = true
	return result
}

func runFixedServiceHealthCheck(service string) ServiceHealthResult {
	command := []string{"systemctl", "is-active", "--quiet", service}
	result := ServiceHealthResult{Name: service, Command: command}
	if !isAllowedHealthService(service) {
		result.Error = "service health check is not allowed"
		return result
	}
	output := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{Command: command})
	result.Output = output.Output
	if output.NotFound {
		result.Error = command[0] + " not found"
		return result
	}
	if output.TimedOut {
		result.Error = "service health check timed out"
		return result
	}
	if output.Err != nil {
		result.Error = output.Err.Error()
		return result
	}
	result.Healthy = true
	return result
}

func isAllowedServiceCommand(command []string) bool {
	return ServiceCommandPolicy{}.AllowsAction(command)
}

func isAllowedHealthService(service string) bool {
	return ServiceCommandPolicy{}.AllowsHealth(service)
}
