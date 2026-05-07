package api

import "testing"

func TestServiceHealthPolicyRequiresHealthyChecks(t *testing.T) {
	policy := NewServiceHealthPolicy()
	if err := policy.RequireHealthy([]ServiceHealthResult{{Name: "caddy", Healthy: true}}); err != nil {
		t.Fatalf("RequireHealthy valid: %v", err)
	}
	if err := policy.RequireHealthy([]ServiceHealthResult{{Name: "caddy", Healthy: false, Error: "unhealthy"}}); err == nil || err.Error() != "unhealthy" {
		t.Fatalf("error err = %v", err)
	}
	if err := policy.RequireHealthy([]ServiceHealthResult{{Name: "hysteria2", Healthy: false}}); err == nil || err.Error() != "hysteria2 health check failed" {
		t.Fatalf("fallback err = %v", err)
	}
}
