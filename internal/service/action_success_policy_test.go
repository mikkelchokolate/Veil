package service

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestActionSuccessPolicyRequiresAllActionsSuccessful(t *testing.T) {
	policy := NewActionSuccessPolicy()
	if err := policy.RequireSuccessful([]model.ServiceActionResult{{Name: "caddy", Success: true}}); err != nil {
		t.Fatalf("RequireSuccessful valid: %v", err)
	}
	if err := policy.RequireSuccessful([]model.ServiceActionResult{{Name: "caddy", Success: false, Error: "restart failed"}}); err == nil || err.Error() != "restart failed" {
		t.Fatalf("error err = %v", err)
	}
	if err := policy.RequireSuccessful([]model.ServiceActionResult{{Name: "hysteria2", Success: false}}); err == nil || err.Error() != "hysteria2 service action failed" {
		t.Fatalf("fallback err = %v", err)
	}
}
