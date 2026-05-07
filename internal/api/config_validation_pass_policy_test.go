package api

import "testing"

func TestConfigValidationPassPolicyRejectsSkippedInvalidAndUsesErrorText(t *testing.T) {
	policy := NewConfigValidationPassPolicy()
	if err := policy.RequirePassed([]ConfigValidationResult{{Name: "caddy", Valid: true}}); err != nil {
		t.Fatalf("RequirePassed valid: %v", err)
	}
	if err := policy.RequirePassed([]ConfigValidationResult{{Name: "caddy", Skipped: true}}); err == nil || err.Error() != "caddy validation did not pass" {
		t.Fatalf("skipped err = %v", err)
	}
	if err := policy.RequirePassed([]ConfigValidationResult{{Name: "hysteria2", Error: "bad yaml"}}); err == nil || err.Error() != "bad yaml" {
		t.Fatalf("error err = %v", err)
	}
}
