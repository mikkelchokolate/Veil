package api

import "testing"

func TestServiceActionSuccessPolicyRequiresAllActionsSuccessful(t *testing.T) {
	policy := NewServiceActionSuccessPolicy()
	if err := policy.RequireSuccessful([]ServiceActionResult{{Name: "caddy", Success: true}}); err != nil {
		t.Fatalf("RequireSuccessful valid: %v", err)
	}
	if err := policy.RequireSuccessful([]ServiceActionResult{{Name: "caddy", Success: false, Error: "restart failed"}}); err == nil || err.Error() != "restart failed" {
		t.Fatalf("error err = %v", err)
	}
	if err := policy.RequireSuccessful([]ServiceActionResult{{Name: "hysteria2", Success: false}}); err == nil || err.Error() != "hysteria2 service action failed" {
		t.Fatalf("fallback err = %v", err)
	}
}
