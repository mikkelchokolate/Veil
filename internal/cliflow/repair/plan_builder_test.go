package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

var testExecutableFunc = func() (string, error) { return "", os.ErrNotExist }
var testLookPath = func(string) (string, error) { return "", os.ErrNotExist }

func buildRepairPlanFromOptions(opts Options) (installer.RepairPlan, error) {
	return BuildPlanFromOptions(opts, PlanDependencies{Secret: func(label string) string { return "repair-" + label }, Executable: testExecutableFunc, LookPath: testLookPath})
}

func TestBuildRepairPlanFromOptionsUsesCurrentExecutableForPanelUnit(t *testing.T) {
	oldExecutable := testExecutableFunc
	testExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	t.Cleanup(func() { testExecutableFunc = oldExecutable })

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()})
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
	oldExecutable := testExecutableFunc
	testExecutableFunc = func() (string, error) { return "/usr/local/bin/veil", nil }
	t.Cleanup(func() { testExecutableFunc = oldExecutable })

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
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

func TestBuildRepairPlanFromOptionsDoesNotReenablePanelTLSForCaddyAccess(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()
	directProfile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelPort: 2096, Secret: func(label string) string { return "direct-" + label }})
	if err != nil {
		t.Fatalf("Build direct profile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(directProfile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("Apply direct profile: %v", err)
	}
	caddyProfile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "caddy-" + label }})
	if err != nil {
		t.Fatalf("Build caddy profile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(caddyProfile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("Apply caddy profile: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	for _, action := range plan.Actions {
		if strings.HasSuffix(action.Path, "veil.env") && strings.Contains(action.Content, "VEIL_TLS_CERT") {
			t.Fatalf("repair should not re-enable direct Panel TLS for caddy Panel access:\n%s", action.Content)
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
	if err := os.Remove(filepath.Join(etcDir, "generated", "caddy", "panel.Caddyfile")); err != nil {
		t.Fatalf("remove panel.Caddyfile: %v", err)
	}
	if err := os.Remove(filepath.Join(systemdDir, "veil-caddy@.service")); err != nil {
		t.Fatalf("remove veil-caddy@.service: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"generated/caddy/panel.Caddyfile", "veil-caddy@.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Panel Caddy repair summary missing %q:\n%s", want, summary)
		}
	}
}

func TestBuildRepairPlanFromOptionsBuildsPanelInstallPlan(t *testing.T) {
	plan, err := buildRepairPlanFromOptions(Options{
		Profile:    "ru-recommended",
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
