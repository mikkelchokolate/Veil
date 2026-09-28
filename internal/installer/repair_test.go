package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/systemdunits"
)

func TestBuildRepairPlanDetectsMissingFiles(t *testing.T) {
	etcDir := filepath.Join(t.TempDir(), "etc", "veil")
	varDir := filepath.Join(t.TempDir(), "var", "lib", "veil")
	systemdDir := filepath.Join(t.TempDir(), "etc", "systemd", "system")

	profile := RURecommendedProfile{
		Domain:            "vpn.example.com",
		InstallPanelCaddy: true,
		CaddyJSON:         "{}",
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

	// Two .veil-managed dir markers (etc+state) join the panel caddy, fallback, and unit set (#1145).
	wantActions := 4 + len(systemdunits.Names())
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
	for _, name := range systemdunits.Names() {
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
		CaddyJSON:         "{\"expected\":true}",
	}

	paths := ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
	}

	// Pre-create Caddy JSON with stale content
	caddyPath := filepath.Join(etcDir, "generated", "caddy", "config.json")
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
			if action.Content != "{\"expected\":true}" {
				t.Fatalf("expected caddy repair content to be '{\"expected\":true}', got %q", action.Content)
			}
			foundDrift = true
		}
	}
	if !foundDrift {
		t.Fatalf("expected drift action for Caddy config, actions: %+v", plan.Actions)
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
		CaddyJSON:         "{}",
	}

	paths := ApplyPaths{
		EtcDir: etcDir,
		VarDir: varDir,
	}

	// Pre-create every managed file with matching content, mode, and
	// ownership — including the .veil-managed markers introduced for
	// uninstall safety (#1145) — so the plan reports no changes.
	desiredFiles, err := desiredManagedFiles(profile, paths)
	if err != nil {
		t.Fatalf("desired files: %v", err)
	}
	for _, file := range desiredFiles {
		if !strings.HasPrefix(file.Path, etcDir+string(os.PathSeparator)) &&
			!strings.HasPrefix(file.Path, varDir+string(os.PathSeparator)) {
			t.Fatalf("desired file outside test roots: %s", file.Path)
		}
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", file.Path, err)
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0o640
		}
		if err := os.WriteFile(file.Path, []byte(file.Content), mode); err != nil {
			t.Fatalf("write %s: %v", file.Path, err)
		}
		if file.Owner != nil {
			if err := os.Chown(file.Path, file.Owner.UID, file.Owner.GID); err != nil {
				t.Fatalf("chown %s: %v", file.Path, err)
			}
		}
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
