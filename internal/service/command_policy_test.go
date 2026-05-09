package service

import "testing"

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
