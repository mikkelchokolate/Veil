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
}

func (r *recordingFirewallRunner) Run(in veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.calls = append(r.calls, in)
	if len(in.Command) == 0 {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("empty command"), Empty: true}
	}
	if len(in.Command) >= 2 && in.Command[1] == "status" {
		return veilruntime.RuntimeCommandOutput{Output: "Status: inactive"}
	}
	if r.failCommand == "enable" && len(in.Command) >= 2 && in.Command[1] == "--force" {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("ufw not found"), NotFound: true, Output: "ufw not found"}
	}
	if r.failCommand == "allow" && len(in.Command) >= 2 && in.Command[1] == "allow" {
		return veilruntime.RuntimeCommandOutput{Err: fmt.Errorf("ufw allow failed"), Output: "failed"}
	}
	return veilruntime.RuntimeCommandOutput{}
}

func setTestUFWApplier(runner *recordingFirewallRunner) func() {
	orig := newUFWApplier
	newUFWApplier = func() firewall.UFWApplier {
		return firewall.NewUFWApplierWithRunner(runner)
	}
	return func() { newUFWApplier = orig }
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
		Profile: profile,
		FirewallActions: []firewall.Rule{
			{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
		},
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
		Profile: profile,
		FirewallActions: []firewall.Rule{
			{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
		},
	}

	_, err := ApplyRURecommendedProfileWithPlan(profile, paths, plan)
	if err == nil {
		t.Fatal("expected error when firewall cannot be enabled")
	}
	if !strings.Contains(err.Error(), "enable firewall") {
		t.Fatalf("expected enable firewall error, got %v", err)
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
		Profile: profile,
		FirewallActions: []firewall.Rule{
			{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
		},
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
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		BackupDir:  backupDir,
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

