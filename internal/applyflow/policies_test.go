package applyflow

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// A skipped validation means a configured validator could not run (e.g. the
// caddy/sing-box binary missed exec.LookPath) — that must block live apply,
// not wave a bad config through to promotion (issue #686).
func TestConfigValidationPassPolicyRejectsSkippedValidation(t *testing.T) {
	policy := NewConfigValidationPassPolicy()

	if err := policy.RequirePassed([]model.ConfigValidationResult{
		{Name: "caddy", Skipped: true, Valid: false, Error: "caddy not found; syntax validation skipped"},
	}); err == nil || err.Error() != "caddy not found; syntax validation skipped" {
		t.Fatalf("skipped validation must fail closed with its error, got %v", err)
	}

	if err := policy.RequirePassed([]model.ConfigValidationResult{
		{Name: "warp", Skipped: true},
	}); err == nil || err.Error() != "warp validation did not pass" {
		t.Fatalf("skipped validation without error text must still fail, got %v", err)
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
