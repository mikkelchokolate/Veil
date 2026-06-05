package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestBuildRepairPlanDetectsMissingFiles(t *testing.T) {
	etcDir := filepath.Join(t.TempDir(), "etc", "veil")
	varDir := filepath.Join(t.TempDir(), "var", "lib", "veil")
	systemdDir := filepath.Join(t.TempDir(), "etc", "systemd", "system")

	profile := RURecommendedProfile{
		Domain:            "vpn.example.com",
		InstallPanelCaddy: true,
		Caddyfile:         "caddy content",
	}

	paths := ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
	}

	plan, err := BuildRepairPlan(profile, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !plan.HasChanges() {
		t.Fatalf("expected repair plan to have changes for missing files, got none")
	}

	wantActions := 2 + len(renderer.ManagedSystemdUnitNames())
	if len(plan.Actions) != wantActions {
		t.Fatalf("expected %d repair actions (panel caddy, fallback, managed systemd units), got %d: %+v", wantActions, len(plan.Actions), plan.Actions)
	}

	for _, action := range plan.Actions {
		if action.Reason != RepairReasonMissing {
			t.Fatalf("expected all actions to be 'missing', got %q for %s", action.Reason, action.Path)
		}
		if action.Content == "" {
			t.Fatalf("expected repair action for %s to have content", action.Path)
		}
	}

	summary := plan.Summary()
	if summary == "No repair actions required\n" {
		t.Fatalf("expected repair summary with actions, got: %q", summary)
	}
	for _, name := range renderer.ManagedSystemdUnitNames() {
		if !containsRepairAction(plan, filepath.Join(systemdDir, name)) {
			t.Fatalf("repair plan missing managed unit %q: %+v", name, plan.Actions)
		}
	}
}

func TestBuildRepairPlanDetectsDriftedFiles(t *testing.T) {
	etcDir := filepath.Join(t.TempDir(), "etc", "veil")
	varDir := filepath.Join(t.TempDir(), "var", "lib", "veil")
	systemdDir := filepath.Join(t.TempDir(), "etc", "systemd", "system")

	profile := RURecommendedProfile{
		Domain:            "vpn.example.com",
		InstallPanelCaddy: true,
		Caddyfile:         "expected caddy content",
	}

	paths := ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
	}

	// Pre-create Caddyfile with stale content
	caddyPath := filepath.Join(etcDir, "generated", "caddy", "panel.Caddyfile")
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(caddyPath, []byte("stale caddy content"), 0o600); err != nil {
		t.Fatalf("write caddy: %v", err)
	}

	plan, err := BuildRepairPlan(profile, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !plan.HasChanges() {
		t.Fatalf("expected repair plan to detect drift")
	}

	foundDrift := false
	for _, action := range plan.Actions {
		if action.Path == caddyPath {
			if action.Reason != RepairReasonDrifted {
				t.Fatalf("expected caddy to be 'drifted', got %q", action.Reason)
			}
			if action.Content != "expected caddy content" {
				t.Fatalf("expected caddy repair content to be 'expected caddy content', got %q", action.Content)
			}
			foundDrift = true
		}
	}
	if !foundDrift {
		t.Fatalf("expected drift action for Caddyfile, actions: %+v", plan.Actions)
	}
}

func containsRepairAction(plan RepairPlan, path string) bool {
	for _, action := range plan.Actions {
		if action.Path == path {
			return true
		}
	}
	return false
}

func TestBuildRepairPlanNoChangesWhenFilesMatch(t *testing.T) {
	etcDir := filepath.Join(t.TempDir(), "etc", "veil")
	varDir := filepath.Join(t.TempDir(), "var", "lib", "veil")

	profile := RURecommendedProfile{
		Domain:            "vpn.example.com",
		InstallPanelCaddy: true,
		Caddyfile:         "caddy content",
	}

	paths := ApplyPaths{
		EtcDir: etcDir,
		VarDir: varDir,
	}

	// Pre-create Caddyfile with matching content
	caddyPath := filepath.Join(etcDir, "generated", "caddy", "panel.Caddyfile")
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(caddyPath, []byte("caddy content"), 0o600); err != nil {
		t.Fatalf("write caddy: %v", err)
	}

	// Pre-create fallback index with matching content
	indexPath := filepath.Join(varDir, "www", "index.html")
	indexContent := ""
	desiredFiles, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desired files: %v", err)
	}
	for _, file := range desiredFiles {
		if file.Path == indexPath {
			indexContent = file.Content
		}
	}
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	plan, err := BuildRepairPlan(profile, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.HasChanges() {
		t.Fatalf("expected no repair actions when files match, got: %+v", plan.Actions)
	}

	if plan.Summary() != "No repair actions required\n" {
		t.Fatalf("expected no-actions summary, got: %q", plan.Summary())
	}
}
