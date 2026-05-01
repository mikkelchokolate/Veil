package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRURecommendedProfileWritesGeneratedFiles(t *testing.T) {
	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	assertFileContains(t, result.CaddyfilePath, "forward_proxy")
	assertFileContains(t, result.Hysteria2Path, "listen: :443")
	assertFileContains(t, result.FallbackIndexPath, "Veil")
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	if len(result.WrittenFiles) != 7 {
		t.Fatalf("expected 7 written files, got %+v", result.WrittenFiles)
	}
}

func TestApplyRURecommendedProfileWritesOnlySelectedStackFiles(t *testing.T) {
	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        StackHysteria2,
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	assertFileContains(t, result.Hysteria2Path, "listen: :443")
	assertFileMissing(t, result.CaddyfilePath)
	assertFileMissing(t, result.FallbackIndexPath)
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	assertFileMissing(t, filepath.Join(dir, "etc", "systemd", "system", "veil-naive.service"))
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil-hysteria2.service"), "hysteria2")
	if len(result.WrittenFiles) != 4 {
		t.Fatalf("expected 4 written files, got %+v", result.WrittenFiles)
	}
}

func TestApplyRURecommendedProfileRejectsMissingPaths(t *testing.T) {
	_, err := ApplyRURecommendedProfile(RURecommendedProfile{}, ApplyPaths{})
	if err == nil {
		t.Fatalf("expected missing paths error")
	}
}

func TestBuildRepairPlanDetectsMissingAndDriftedFiles(t *testing.T) {
	dir := t.TempDir()
	profile := mustRUProfile(t, StackBoth)
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
	profile := mustRUProfile(t, StackBoth)
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

func mustRUProfile(t *testing.T, stack Stack) RURecommendedProfile {
	t.Helper()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        stack,
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	return profile
}

func assertRepairAction(t *testing.T, plan RepairPlan, path string, reason RepairReason) {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Path == path && action.Reason == reason {
			return
		}
	}
	t.Fatalf("missing repair action path=%s reason=%s in %+v", path, reason, plan.Actions)
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s to be absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), want) {
		t.Fatalf("file %s missing %q:\n%s", path, want, string(body))
	}
}
