package service

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type CommandPolicy struct {
	catalog ManagedRuntimeCatalog
}

func NewCommandPolicy(catalog ManagedRuntimeCatalog) CommandPolicy {
	return CommandPolicy{catalog: catalog}
}

func (p CommandPolicy) AllowsAction(command []string) bool {
	return p.catalog.AllowsPromotedAction(command)
}

func (p CommandPolicy) AllowsReload(command []string) bool {
	return p.AllowsAction(command)
}

func (p CommandPolicy) AllowsHealth(service string) bool {
	return p.catalog.AllowsHealthUnit(service)
}

type RuntimeCommandRunner interface {
	Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput
}

func RunFixedServiceAction(command []string, policy CommandPolicy, runner RuntimeCommandRunner) model.ServiceActionResult {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	result := model.ServiceActionResult{Command: append([]string(nil), command...)}
	if len(command) > 0 {
		result.Name = command[len(command)-1]
	}
	if !policy.AllowsAction(command) {
		result.Error = "service command is not allowed"
		return result
	}
	output := runner.Run(veilruntime.RuntimeCommandInput{Command: command})
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

func RunFixedServiceHealthCheck(serviceName string, policy CommandPolicy, runner RuntimeCommandRunner) model.ServiceHealthResult {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	command := []string{"systemctl", "is-active", "--quiet", serviceName}
	result := model.ServiceHealthResult{Name: serviceName, Command: command}
	if !policy.AllowsHealth(serviceName) {
		result.Error = "service health check is not allowed"
		return result
	}
	output := runner.Run(veilruntime.RuntimeCommandInput{Command: command})
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
