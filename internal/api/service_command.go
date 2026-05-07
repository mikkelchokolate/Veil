package api

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type ServiceCommandPolicy struct{}

func (p ServiceCommandPolicy) AllowsAction(command []string) bool {
	if len(command) != 3 || command[0] != "systemctl" {
		return false
	}
	if command[1] == "reload" {
		return p.AllowsHealth(command[2])
	}
	return command[1] == "restart" && command[2] == "veil-mieru.service"
}

func (p ServiceCommandPolicy) AllowsReload(command []string) bool {
	return p.AllowsAction(command)
}

func (ServiceCommandPolicy) AllowsHealth(service string) bool {
	return service == "veil-naive.service" || service == "veil-hysteria2.service" || service == "veil-warp.service" || service == "veil-mieru.service"
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
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.Error = command[0] + " not found"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "service action timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
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
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.Error = command[0] + " not found"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "service health check timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
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
