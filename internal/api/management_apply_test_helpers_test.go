package api

import "testing"

func stubManagementApplySideEffects(t *testing.T) {
	t.Helper()
	originalValidator := stagedConfigValidator
	originalRunner := serviceActionRunner
	originalHealth := serviceHealthChecker
	originalFirewall := firewallApplierInstance
	t.Cleanup(func() {
		stagedConfigValidator = originalValidator
		serviceActionRunner = originalRunner
		serviceHealthChecker = originalHealth
		firewallApplierInstance = originalFirewall
	})
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: path, Config: path, Valid: true})
		}
		return results
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Command: command, Success: true}
	}
	serviceHealthChecker = func(name string) ServiceHealthResult {
		return ServiceHealthResult{Name: name, Healthy: true}
	}
	firewallApplierInstance = &fakeFirewallApplier{}
}
