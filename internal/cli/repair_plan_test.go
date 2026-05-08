package cli

import (
	"strings"
	"testing"
)

func TestBuildRepairPlanFromOptionsBuildsMieruPlanWithoutDomainOrPort(t *testing.T) {
	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{
		Profile:    "ru-recommended",
		Stack:      "mieru",
		EtcDir:     t.TempDir(),
		VarDir:     t.TempDir(),
		SystemdDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions Mieru: %v", err)
	}
	summary := plan.Summary()
	if !strings.Contains(summary, "veil-mieru.service") || strings.Contains(summary, "generated/caddy/Caddyfile") || strings.Contains(summary, "hysteria2") {
		t.Fatalf("unexpected Mieru repair summary:\n%s", summary)
	}
}

func TestBuildRepairPlanFromOptionsBuildsRURecommendedPlan(t *testing.T) {
	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{
		Profile:    "ru-recommended",
		Stack:      "hysteria2",
		Domain:     "example.com",
		Email:      "admin@example.com",
		SharedPort: 443,
		EtcDir:     t.TempDir(),
		VarDir:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	if !strings.Contains(summary, "hysteria2") || strings.Contains(summary, "generated/caddy/Caddyfile") {
		t.Fatalf("unexpected hysteria2 repair summary:\n%s", summary)
	}
}
