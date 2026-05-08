package cli

import (
	"strings"
	"testing"
)

func TestBuildRepairPlanFromOptionsUsesCurrentExecutableForPanelUnit(t *testing.T) {
	oldExecutable := installExecutableFunc
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	t.Cleanup(func() { installExecutableFunc = oldExecutable })

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", Stack: "panel", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	for _, action := range plan.Actions {
		if strings.HasSuffix(action.Path, "veil.service") && strings.Contains(action.Content, "ExecStart=/opt/veil/bin/veil serve") {
			return
		}
	}
	t.Fatalf("repair plan did not render veil.service with selected binary: %+v", plan.Actions)
}

func TestBuildRepairPlanFromOptionsBuildsPanelInstallPlan(t *testing.T) {
	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{
		Profile:    "ru-recommended",
		Stack:      "panel",
		EtcDir:     t.TempDir(),
		VarDir:     t.TempDir(),
		SystemdDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"veil.service", "panel/tls.crt", "veil.env"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("repair summary missing %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"generated/caddy/Caddyfile", "hysteria2", "veil-mieru.service"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("Panel install repair should not include %q:\n%s", unwanted, summary)
		}
	}
}
