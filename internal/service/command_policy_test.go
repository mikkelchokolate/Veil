package service

import "testing"

func TestRunFixedServiceActionRejectsDisallowedCommands(t *testing.T) {
	policy := NewCommandPolicy(NewManagedRuntimeCatalog([]ManagedRuntime{{Unit: "veil-naive.service", PromotedVerb: "reload"}}))
	tests := []struct {
		name    string
		command []string
		wantErr string
	}{
		{"wrong binary", []string{"bash", "reload", "veil-naive.service"}, "service command is not allowed"},
		{"wrong subcommand", []string{"systemctl", "restart", "veil-naive.service"}, "service command is not allowed"},
		{"wrong service", []string{"systemctl", "reload", "evil.service"}, "service command is not allowed"},
		{"too few args", []string{"systemctl", "reload"}, "service command is not allowed"},
		{"too many args", []string{"systemctl", "reload", "veil-naive.service", "extra"}, "service command is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunFixedServiceAction(tt.command, policy, nil)
			if result.Success {
				t.Fatal("expected failure for disallowed command")
			}
			if result.Error != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, result.Error)
			}
			if result.Name != tt.command[len(tt.command)-1] {
				t.Fatalf("expected name from last arg, got %q", result.Name)
			}
		})
	}
}

func TestRunFixedServiceHealthCheckRejectsDisallowedServices(t *testing.T) {
	policy := NewCommandPolicy(NewManagedRuntimeCatalog([]ManagedRuntime{{Unit: "veil-naive.service", HealthCheckAfter: true}}))
	tests := []struct {
		name    string
		service string
		wantErr string
	}{
		{"unknown service", "unknown.service", "service health check is not allowed"},
		{"nginx service", "nginx.service", "service health check is not allowed"},
		{"empty service", "", "service health check is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunFixedServiceHealthCheck(tt.service, policy, nil)
			if result.Healthy {
				t.Fatal("expected not healthy for disallowed service")
			}
			if result.Error != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, result.Error)
			}
			if result.Name != tt.service {
				t.Fatalf("expected name %q, got %q", tt.service, result.Name)
			}
			expectedCommand := []string{"systemctl", "is-active", "--quiet", tt.service}
			if len(result.Command) != len(expectedCommand) {
				t.Fatalf("expected command %v, got %v", expectedCommand, result.Command)
			}
		})
	}
}

func TestServiceCommandPolicyAllowsPromotedActionsAndHealth(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{{Unit: "veil-mieru.service", PromotedVerb: "restart", HealthCheckAfter: true}})
	policy := NewCommandPolicy(catalog)
	if !policy.AllowsAction([]string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatalf("expected promoted action allowed")
	}
	if policy.AllowsAction([]string{"systemctl", "reload", "caddy.service"}) {
		t.Fatalf("unexpected caddy action allowed")
	}
	if !policy.AllowsHealth("veil-mieru.service") {
		t.Fatalf("expected health allowed")
	}
}
