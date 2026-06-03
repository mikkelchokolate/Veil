package service

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestServiceHealthCollectionChecksOnlySuccessfulNamedActions(t *testing.T) {
	var checked []string
	collection := NewServiceHealthCollection(func(name string) model.ServiceHealthResult {
		checked = append(checked, name)
		return model.ServiceHealthResult{Name: name, Healthy: true}
	})
	checks := collection.Check([]model.ServiceActionResult{
		{Name: "caddy", Success: true},
		{Name: "", Success: true},
		{Name: "hysteria2", Success: false},
		{Name: "sing-box", Success: true},
	})
	if len(checks) != 2 || checks[0].Name != "caddy" || checks[1].Name != "sing-box" {
		t.Fatalf("checks = %+v", checks)
	}
	if len(checked) != 2 || checked[0] != "caddy" || checked[1] != "sing-box" {
		t.Fatalf("checked = %+v", checked)
	}
}

func TestHealthPolicyRequiresHealthyChecks(t *testing.T) {
	policy := NewHealthPolicy()
	if err := policy.RequireHealthy([]model.ServiceHealthResult{{Name: "caddy", Healthy: true}}); err != nil {
		t.Fatalf("RequireHealthy valid: %v", err)
	}
	if err := policy.RequireHealthy([]model.ServiceHealthResult{{Name: "caddy", Healthy: false, Error: "unhealthy"}}); err == nil || err.Error() != "unhealthy" {
		t.Fatalf("error err = %v", err)
	}
	if err := policy.RequireHealthy([]model.ServiceHealthResult{{Name: "hysteria2", Healthy: false}}); err == nil || err.Error() != "hysteria2 health check failed" {
		t.Fatalf("fallback err = %v", err)
	}
}
