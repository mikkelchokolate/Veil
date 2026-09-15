package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type recordingFirewallRunner struct {
	calls       []veilruntime.RuntimeCommandInput
	failCommand string
	enabled     bool
}

func (r *recordingFirewallRunner) Run(in veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.calls = append(r.calls, in)
	if len(in.Command) == 0 {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("empty command"), Empty: true}
	}
	if len(in.Command) >= 2 && in.Command[1] == "status" {
		status := "Status: inactive"
		if r.enabled {
			status = "Status: active"
		}
		return veilruntime.RuntimeCommandOutput{Output: status}
	}
	if r.failCommand == "enable" && len(in.Command) >= 3 && in.Command[1] == "--force" && in.Command[2] == "enable" {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("ufw not found"), NotFound: true, Output: "ufw not found"}
	}
	if r.failCommand == "allow" && len(in.Command) >= 2 && in.Command[1] == "allow" {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("ufw allow failed"), Output: "failed"}
	}
	if len(in.Command) >= 3 && in.Command[1] == "--force" && in.Command[2] == "enable" {
		r.enabled = true
	}
	if len(in.Command) >= 3 && in.Command[1] == "--force" && in.Command[2] == "disable" {
		r.enabled = false
	}
	return veilruntime.RuntimeCommandOutput{}
}

func testFirewallActions() []firewall.Rule {
	return []firewall.Rule{
		{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "Veil management SSH"}},
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
	}
}

func setTestUFWApplier(runner *recordingFirewallRunner) func() {
	orig := newUFWApplier
	newUFWApplier = func() firewall.UFWApplier {
		return firewall.NewUFWApplierWithRunner(runner)
	}
	return func() { newUFWApplier = orig }
}

func TestApplyContinuesWhenQUICBufferTuningFails(t *testing.T) {
	orig := applyQUICUDPBuffers
	origWarn := quicBufferWarn
	applyQUICUDPBuffers = func() error { return fmt.Errorf("sysctl rejected") }
	var warned string
	quicBufferWarn = func(format string, args ...any) {
		warned = fmt.Sprintf(format, args...)
	}
	t.Cleanup(func() {
		applyQUICUDPBuffers = orig
		quicBufferWarn = origWarn
	})

	dir := t.TempDir()
	result, err := ApplyRURecommendedProfile(RURecommendedProfile{PanelAuthToken: "secret-panel"}, ApplyPaths{
		EtcDir: filepath.Join(dir, "etc", "veil"),
		VarDir: filepath.Join(dir, "var", "lib", "veil"),
	})
	if err != nil {
		t.Fatalf("install must continue after QUIC tuning failure: %v", err)
	}
	if len(result.WrittenFiles) == 0 {
		t.Fatal("expected managed files to be written")
	}
	if !strings.Contains(warned, "sysctl rejected") {
		t.Fatalf("expected QUIC warning, got %q", warned)
	}
}

func TestNewInstallApplyWithPlanAppliesWithoutFirewallActions(t *testing.T) {
	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil")}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	plan := InstallPlan{Profile: profile}

	apply := NewInstallApplyWithPlan(profile, paths, plan)
	result, err := apply.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.WrittenFiles) == 0 {
		t.Fatalf("expected files to be written")
	}
}

func TestApplyRURecommendedProfileWithPlanInvokesFirewallApplier(t *testing.T) {
	runner := &recordingFirewallRunner{}
	cleanup := setTestUFWApplier(runner)
	defer cleanup()

	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil")}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	plan := InstallPlan{
		Profile:         profile,
		FirewallActions: testFirewallActions(),
	}

	result, err := ApplyRURecommendedProfileWithPlan(profile, paths, plan)
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfileWithPlan: %v", err)
	}
	if len(result.WrittenFiles) == 0 {
		t.Fatalf("expected files to be written")
	}

	var foundStatus, foundAllow bool
	for _, call := range runner.calls {
		if len(call.Command) >= 2 && call.Command[1] == "status" {
			foundStatus = true
		}
		if len(call.Command) >= 2 && call.Command[1] == "allow" {
			foundAllow = true
		}
	}
	if !foundStatus {
		t.Fatalf("expected ufw status check, got %+v", runner.calls)
	}
	if !foundAllow {
		t.Fatalf("expected ufw allow rule, got %+v", runner.calls)
	}
}

func TestApplyReturnsErrorWhenFirewallEnsureActiveFails(t *testing.T) {
	runner := &recordingFirewallRunner{failCommand: "enable"}
	cleanup := setTestUFWApplier(runner)
	defer cleanup()

	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil")}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	plan := InstallPlan{
		Profile:         profile,
		FirewallActions: testFirewallActions(),
	}

	_, err := ApplyRURecommendedProfileWithPlan(profile, paths, plan)
	if err == nil {
		t.Fatal("expected error when firewall cannot be enabled")
	}
	if !strings.Contains(err.Error(), "apply firewall rules") {
		t.Fatalf("expected apply firewall rules error, got %v", err)
	}
}

func TestApplyReturnsErrorWhenFirewallApplyRulesFails(t *testing.T) {
	runner := &recordingFirewallRunner{failCommand: "allow"}
	cleanup := setTestUFWApplier(runner)
	defer cleanup()

	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil")}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	plan := InstallPlan{
		Profile:         profile,
		FirewallActions: testFirewallActions(),
	}

	_, err := ApplyRURecommendedProfileWithPlan(profile, paths, plan)
	if err == nil {
		t.Fatal("expected error when firewall rules cannot be applied")
	}
	if !strings.Contains(err.Error(), "apply firewall rules") {
		t.Fatalf("expected apply firewall rules error, got %v", err)
	}
}

func TestApplyReturnsErrorWhenBackupDirCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup-is-file")
	if err := os.WriteFile(backupDir, []byte("block"), 0o600); err != nil {
		t.Fatalf("create backup blocker: %v", err)
	}

	paths := ApplyPaths{
		EtcDir:    filepath.Join(dir, "etc", "veil"),
		VarDir:    filepath.Join(dir, "var", "lib", "veil"),
		BackupDir: backupDir,
	}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}

	_, err := ApplyRURecommendedProfile(profile, paths)
	if err == nil {
		t.Fatal("expected error when backup directory cannot be created")
	}
}

func TestApplyReturnsErrorWhenWriteManagedFileFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "etc")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatalf("create etc blocker: %v", err)
	}

	paths := ApplyPaths{
		EtcDir: blocker,
		VarDir: filepath.Join(dir, "var", "lib", "veil"),
	}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}

	_, err := ApplyRURecommendedProfile(profile, paths)
	if err == nil {
		t.Fatal("expected error when managed file cannot be written")
	}
}
