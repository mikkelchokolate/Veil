package applyflow

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestConfigValidationPassPolicyTreatsSkippedAsPass(t *testing.T) {
	policy := NewConfigValidationPassPolicy()

	// A skipped validation (validator binary absent or no standalone checker)
	// must not block the apply — the service health check is the real gate.
	if err := policy.RequirePassed([]model.ConfigValidationResult{
		{Name: "mieru", Skipped: true, Valid: false, Error: "mita not found; syntax validation skipped"},
	}); err != nil {
		t.Fatalf("skipped validation must pass, got %v", err)
	}

	// A passing validation passes.
	if err := policy.RequirePassed([]model.ConfigValidationResult{
		{Name: "caddy", Valid: true},
	}); err != nil {
		t.Fatalf("valid validation must pass, got %v", err)
	}

	// A validation that actually ran and failed must block.
	err := policy.RequirePassed([]model.ConfigValidationResult{
		{Name: "caddy", Valid: false, Skipped: false, Error: "adapter error"},
	})
	if err == nil || err.Error() != "adapter error" {
		t.Fatalf("failed validation must block with its error, got %v", err)
	}
}
