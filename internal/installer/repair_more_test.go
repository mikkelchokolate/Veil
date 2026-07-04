package installer

import (
	"strings"
	"testing"
)

func TestBuildRepairPlanPropagatesDesiredFilesError(t *testing.T) {
	_, err := BuildRepairPlan(RURecommendedProfile{}, ApplyPaths{})
	if err == nil {
		t.Fatalf("expected error when etc dir and var dir are missing")
	}
	if !strings.Contains(err.Error(), "etc dir is required") {
		t.Fatalf("expected error containing %q, got %v", "etc dir is required", err)
	}
}
