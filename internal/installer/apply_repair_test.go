package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRepairPlanDetectsPanelCaddyDrift(t *testing.T) {
	dir := t.TempDir()
	profile := mustPanelCaddyProfile(t)
	paths := ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	}
	result, err := ApplyRURecommendedProfile(profile, paths)
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if err := os.WriteFile(result.CaddyJSONPath, []byte("drifted"), 0o600); err != nil {
		t.Fatalf("drift caddy json: %v", err)
	}

	plan, err := BuildRepairPlan(profile, paths)

	if err != nil {
		t.Fatalf("build repair plan: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 repair action, got %+v", plan.Actions)
	}
	assertRepairAction(t, plan, result.CaddyJSONPath, RepairReasonDrifted)
	if plan.HasChanges() != true {
		t.Fatalf("expected plan to report changes")
	}
	if !strings.Contains(plan.Summary(), "repair drifted") {
		t.Fatalf("summary missing repair reason:\n%s", plan.Summary())
	}
}

func TestApplyRepairPlanWritesOnlyPlannedFiles(t *testing.T) {
	dir := t.TempDir()
	profile := mustPanelCaddyProfile(t)
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd")}
	result, err := ApplyRURecommendedProfile(profile, paths)
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if err := os.WriteFile(result.CaddyJSONPath, []byte("drifted"), 0o600); err != nil {
		t.Fatalf("drift caddy json: %v", err)
	}
	plan, err := BuildRepairPlan(profile, paths)
	if err != nil {
		t.Fatalf("build repair plan: %v", err)
	}

	repairResult, err := ApplyRepairPlan(plan)

	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if len(repairResult.WrittenFiles) != 1 || repairResult.WrittenFiles[0] != result.CaddyJSONPath {
		t.Fatalf("unexpected repaired files: %+v", repairResult.WrittenFiles)
	}
	assertFileContains(t, result.CaddyJSONPath, `"dial": "127.0.0.1:2096"`)
}
