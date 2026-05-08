package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
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

func TestBuildRepairPlanFromOptionsPreservesExistingPanelSecrets(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{Secret: func(label string) string { return "original-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(profile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	oldExecutable := installExecutableFunc
	installExecutableFunc = func() (string, error) { return "/usr/local/bin/veil", nil }
	t.Cleanup(func() { installExecutableFunc = oldExecutable })

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", Stack: "panel", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, unwanted := range []string{"veil.env", "panel/tls.crt", "panel/tls.key"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("repair should preserve existing panel secret material, but planned %q:\n%s", unwanted, summary)
		}
	}
}

func TestBuildRepairPlanFromOptionsRepairsExistingPanelCaddyAccess(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "original-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(profile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if err := os.Remove(filepath.Join(etcDir, "generated", "caddy", "Caddyfile")); err != nil {
		t.Fatalf("remove Caddyfile: %v", err)
	}
	if err := os.Remove(filepath.Join(systemdDir, "veil-naive.service")); err != nil {
		t.Fatalf("remove veil-naive.service: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(repairWorkflowOptions{Profile: "ru-recommended", Stack: "panel", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"generated/caddy/Caddyfile", "veil-naive.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Panel Caddy repair summary missing %q:\n%s", want, summary)
		}
	}
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
