package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _apply_repair_deps = []any{
	os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestBuildRepairPlanDetectsMissingAndDriftedFiles(t *testing.T) {
	dir := t.TempDir()
	profile := mustRUProfile(t, Stack("both"))
	paths := ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	}
	result, err := ApplyRURecommendedProfile(profile, paths)
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if err := os.WriteFile(result.CaddyfilePath, []byte("drifted"), 0o600); err != nil {
		t.Fatalf("drift caddyfile: %v", err)
	}
	if err := os.Remove(result.Hysteria2Path); err != nil {
		t.Fatalf("remove hysteria config: %v", err)
	}

	plan, err := BuildRepairPlan(profile, paths)

	if err != nil {
		t.Fatalf("build repair plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("expected 2 repair actions, got %+v", plan.Actions)
	}
	assertRepairAction(t, plan, result.CaddyfilePath, RepairReasonDrifted)
	assertRepairAction(t, plan, result.Hysteria2Path, RepairReasonMissing)
	if plan.HasChanges() != true {
		t.Fatalf("expected plan to report changes")
	}
	if !strings.Contains(plan.Summary(), "repair drifted") || !strings.Contains(plan.Summary(), "repair missing") {
		t.Fatalf("summary missing repair reasons:\n%s", plan.Summary())
	}
}

func TestApplyRepairPlanWritesOnlyPlannedFiles(t *testing.T) {
	dir := t.TempDir()
	profile := mustRUProfile(t, Stack("both"))
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd")}
	result, err := ApplyRURecommendedProfile(profile, paths)
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}
	if err := os.WriteFile(result.CaddyfilePath, []byte("drifted"), 0o600); err != nil {
		t.Fatalf("drift caddyfile: %v", err)
	}
	plan, err := BuildRepairPlan(profile, paths)
	if err != nil {
		t.Fatalf("build repair plan: %v", err)
	}

	repairResult, err := ApplyRepairPlan(plan)

	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if len(repairResult.WrittenFiles) != 1 || repairResult.WrittenFiles[0] != result.CaddyfilePath {
		t.Fatalf("unexpected repaired files: %+v", repairResult.WrittenFiles)
	}
	assertFileContains(t, result.CaddyfilePath, "forward_proxy")
}

func TestBuildBinaryRepairPlanRequiresChecksumAndDetectsMissingBinary(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "hysteria")
	_, err := BuildBinaryRepairPlan(BinaryAcquisition{Name: "hysteria2", URL: "https://example.com/hysteria", Destination: dest})
	if err == nil {
		t.Fatalf("expected checksum requirement error")
	}
	checksum, err := SHA256Hex([]byte("binary-body"))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	plan, err := BuildBinaryRepairPlan(BinaryAcquisition{Name: "hysteria2", URL: "https://example.com/hysteria", Destination: dest, SHA256: checksum})

	if err != nil {
		t.Fatalf("build binary repair plan: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Reason != RepairReasonMissing || plan.Actions[0].Destination != dest {
		t.Fatalf("unexpected binary repair plan: %+v", plan)
	}
	if !strings.Contains(plan.Summary(), "repair missing binary hysteria2") {
		t.Fatalf("summary missing binary repair action:\n%s", plan.Summary())
	}
}

func TestBuildBinaryRepairPlanDetectsChecksumDrift(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "hysteria")
	if err := os.WriteFile(dest, []byte("old-body"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	checksum, err := SHA256Hex([]byte("new-body"))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	plan, err := BuildBinaryRepairPlan(BinaryAcquisition{Name: "hysteria2", URL: "https://example.com/hysteria", Destination: dest, SHA256: checksum})

	if err != nil {
		t.Fatalf("build binary repair plan: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Reason != RepairReasonDrifted {
		t.Fatalf("expected drifted binary action, got %+v", plan)
	}
}

func TestBuildBinaryRepairPlanEmptyName(t *testing.T) {
	_, err := BuildBinaryRepairPlan(BinaryAcquisition{URL: "https://example.com/hysteria", Destination: "/tmp/hysteria", SHA256: "abc123"})
	if err == nil {
		t.Fatalf("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "binary name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildBinaryRepairPlanEmptyURL(t *testing.T) {
	_, err := BuildBinaryRepairPlan(BinaryAcquisition{Name: "hysteria2", Destination: "/tmp/hysteria", SHA256: "abc123"})
	if err == nil {
		t.Fatalf("expected error for empty url")
	}
	if !strings.Contains(err.Error(), "binary url is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildBinaryRepairPlanEmptyDestination(t *testing.T) {
	_, err := BuildBinaryRepairPlan(BinaryAcquisition{Name: "hysteria2", URL: "https://example.com/hysteria", SHA256: "abc123"})
	if err == nil {
		t.Fatalf("expected error for empty destination")
	}
	if !strings.Contains(err.Error(), "binary destination is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildBinaryRepairPlanCaseInsensitiveSHA256(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "hysteria")
	body := []byte("test-binary-body")
	if err := os.WriteFile(dest, body, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	checksum, err := SHA256Hex(body)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	upperChecksum := strings.ToUpper(checksum)

	plan, err := BuildBinaryRepairPlan(BinaryAcquisition{Name: "hysteria2", URL: "https://example.com/hysteria", Destination: dest, SHA256: upperChecksum})

	if err != nil {
		t.Fatalf("build binary repair plan: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("expected empty plan for matching SHA256 (case-insensitive), got actions: %+v", plan)
	}
}
