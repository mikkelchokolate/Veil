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

func TestApplyWithBackupDirBacksUpExistingFilesBeforeOverwrite(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

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

	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create a file that will be overwritten
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	oldCaddyPath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(oldCaddyPath, []byte("old caddy content"), 0o600); err != nil {
		t.Fatalf("write old caddy: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
		BackupDir:  backupDir,
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	if result.BackupID == "" {
		t.Fatalf("expected BackupID to be set when BackupDir is provided")
	}

	// Verify backup contains old Caddyfile
	backupPath := filepath.Join(backupDir, result.BackupID, "Caddyfile")
	body, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup caddyfile: %v", err)
	}
	if string(body) != "old caddy content" {
		t.Fatalf("backup has wrong content: %q", string(body))
	}

	// Verify current Caddyfile has new content
	assertFileContains(t, oldCaddyPath, "forward_proxy")
}

func TestApplyWithoutBackupDirDoesNotBackup(t *testing.T) {
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

	if result.BackupID != "" {
		t.Fatalf("expected BackupID to be empty when BackupDir is not set, got %q", result.BackupID)
	}
}

func TestApplyBackupThenRestoreRollback(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

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

	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create files that will be overwritten
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	oldCaddyPath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(oldCaddyPath, []byte("pre-apply content"), 0o600); err != nil {
		t.Fatalf("write old caddy: %v", err)
	}

	veilEnvPath := filepath.Join(etcDir, "veil.env")
	if err := os.MkdirAll(filepath.Dir(veilEnvPath), 0o755); err != nil {
		t.Fatalf("mkdir veil env dir: %v", err)
	}
	if err := os.WriteFile(veilEnvPath, []byte("VEIL_API_TOKEN=old-token\n"), 0o600); err != nil {
		t.Fatalf("write old env: %v", err)
	}

	// Apply with backup
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
		BackupDir:  backupDir,
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	// Verify files were overwritten
	body, _ := os.ReadFile(oldCaddyPath)
	if string(body) == "pre-apply content" {
		t.Fatalf("Caddyfile should have been overwritten")
	}
	assertFileContains(t, oldCaddyPath, "forward_proxy")

	body, _ = os.ReadFile(veilEnvPath)
	if string(body) == "VEIL_API_TOKEN=old-token\n" {
		t.Fatalf("veil.env should have been overwritten")
	}
	assertFileContains(t, veilEnvPath, "VEIL_API_TOKEN=secret-panel")

	// Rollback: restore from backup
	restored, err := RestoreFromBackup(backupDir, result.BackupID)
	if err != nil {
		t.Fatalf("restore from backup: %v", err)
	}
	if len(restored) < 2 {
		t.Fatalf("expected at least 2 restored files, got %d: %v", len(restored), restored)
	}

	// Verify original content is back
	body, err = os.ReadFile(oldCaddyPath)
	if err != nil {
		t.Fatalf("read restored caddy: %v", err)
	}
	if string(body) != "pre-apply content" {
		t.Fatalf("restored Caddyfile has wrong content: %q", string(body))
	}

	body, err = os.ReadFile(veilEnvPath)
	if err != nil {
		t.Fatalf("read restored veil.env: %v", err)
	}
	if string(body) != "VEIL_API_TOKEN=old-token\n" {
		t.Fatalf("restored veil.env has wrong content: %q", string(body))
	}

	// Cleanup backup
	if err := CleanupBackup(backupDir, result.BackupID); err != nil {
		t.Fatalf("cleanup backup: %v", err)
	}

	// Verify backup is gone
	if _, err := os.Stat(filepath.Join(backupDir, result.BackupID)); !os.IsNotExist(err) {
		t.Fatalf("backup should be cleaned up")
	}
}

func TestApplyBackupSkipsNewFilesThatDidNotExist(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

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
		BackupDir:  backupDir,
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	if result.BackupID == "" {
		t.Fatalf("expected BackupID to be set")
	}

	// All files are new, so backup should be nearly empty
	backupPath := filepath.Join(backupDir, result.BackupID)
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	fileCount := 0
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			fileCount++
		}
	}
	if fileCount != 0 {
		t.Fatalf("expected no backed up files for fresh apply, got %d: %v", fileCount, entries)
	}
}

func TestWriteManagedFileFailsWhenParentIsNotDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where a directory is needed
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	// Try to write a file under blocker/subdir/ — MkdirAll should fail with ENOTDIR
	path := filepath.Join(blocker, "subdir", "file.txt")
	err := writeManagedFile(path, "content", 0o600)
	if err == nil {
		t.Fatal("expected error writing file under non-directory path")
	}
}
