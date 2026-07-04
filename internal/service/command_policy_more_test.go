package service

import (
	"errors"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeCommandPolicyRunner struct {
	out veilruntime.RuntimeCommandOutput
}

func (r *fakeCommandPolicyRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	return r.out
}

func TestCommandPolicyAllowsReload(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{{Unit: "veil-mieru.service", PromotedVerb: "restart"}})
	policy := NewCommandPolicy(catalog)
	if !policy.AllowsReload([]string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatal("expected AllowsReload to delegate to AllowsAction")
	}
	if policy.AllowsReload([]string{"systemctl", "reload", "veil-mieru.service"}) {
		t.Fatal("expected disallowed reload to be rejected")
	}
}

func TestRunFixedServiceActionSuccessAndFailures(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{{Unit: "veil-mieru.service", PromotedVerb: "restart"}})
	policy := NewCommandPolicy(catalog)
	command := []string{"systemctl", "restart", "veil-mieru.service"}

	tests := []struct {
		name        string
		out         veilruntime.RuntimeCommandOutput
		wantSuccess bool
		wantError   string
	}{
		{
			name:        "success",
			out:         veilruntime.RuntimeCommandOutput{Output: "ok"},
			wantSuccess: true,
			wantError:   "",
		},
		{
			name:        "not found",
			out:         veilruntime.RuntimeCommandOutput{Output: "", NotFound: true, Err: errors.New("systemctl not found")},
			wantSuccess: false,
			wantError:   "systemctl not found",
		},
		{
			name:        "timed out",
			out:         veilruntime.RuntimeCommandOutput{Output: "", TimedOut: true, Err: errors.New("context deadline exceeded")},
			wantSuccess: false,
			wantError:   "service action timed out",
		},
		{
			name:        "error",
			out:         veilruntime.RuntimeCommandOutput{Output: "boom", Err: errors.New("exit status 1")},
			wantSuccess: false,
			wantError:   "exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunFixedServiceAction(command, policy, &fakeCommandPolicyRunner{out: tt.out})
			if result.Success != tt.wantSuccess {
				t.Fatalf("Success = %v, want %v", result.Success, tt.wantSuccess)
			}
			if result.Error != tt.wantError {
				t.Fatalf("Error = %q, want %q", result.Error, tt.wantError)
			}
			if result.Name != "veil-mieru.service" {
				t.Fatalf("Name = %q, want veil-mieru.service", result.Name)
			}
			if !sameCommand(result.Command, command) {
				t.Fatalf("Command = %v, want %v", result.Command, command)
			}
		})
	}
}

func TestRunFixedServiceHealthCheckSuccessAndFailures(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{{Unit: "veil-mieru.service", HealthCheckAfter: true}})
	policy := NewCommandPolicy(catalog)

	tests := []struct {
		name        string
		out         veilruntime.RuntimeCommandOutput
		wantHealthy bool
		wantError   string
	}{
		{
			name:        "healthy",
			out:         veilruntime.RuntimeCommandOutput{Output: ""},
			wantHealthy: true,
		},
		{
			name:        "not found",
			out:         veilruntime.RuntimeCommandOutput{Output: "", NotFound: true, Err: errors.New("systemctl not found")},
			wantHealthy: false,
			wantError:   "systemctl not found",
		},
		{
			name:        "timed out",
			out:         veilruntime.RuntimeCommandOutput{Output: "", TimedOut: true, Err: errors.New("context deadline exceeded")},
			wantHealthy: false,
			wantError:   "service health check timed out",
		},
		{
			name:        "error",
			out:         veilruntime.RuntimeCommandOutput{Output: "inactive", Err: errors.New("exit status 3")},
			wantHealthy: false,
			wantError:   "exit status 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunFixedServiceHealthCheck("veil-mieru.service", policy, &fakeCommandPolicyRunner{out: tt.out})
			if result.Healthy != tt.wantHealthy {
				t.Fatalf("Healthy = %v, want %v", result.Healthy, tt.wantHealthy)
			}
			if result.Error != tt.wantError {
				t.Fatalf("Error = %q, want %q", result.Error, tt.wantError)
			}
			wantCommand := []string{"systemctl", "is-active", "--quiet", "veil-mieru.service"}
			if !sameCommand(result.Command, wantCommand) {
				t.Fatalf("Command = %v, want %v", result.Command, wantCommand)
			}
		})
	}
}
