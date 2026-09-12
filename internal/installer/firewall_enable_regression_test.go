package installer

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

func TestInstallApplyStagesAllowRulesBeforeEnable(t *testing.T) {
	runner := &recordingFirewallRunner{}
	cleanup := setTestUFWApplier(runner)
	defer cleanup()

	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: dir + "/etc/veil", VarDir: dir + "/var/lib/veil"}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	plan := InstallPlan{Profile: profile, FirewallActions: testFirewallActions()}

	if _, err := ApplyRURecommendedProfileWithPlan(profile, paths, plan); err != nil {
		t.Fatalf("ApplyRURecommendedProfileWithPlan: %v", err)
	}

	var sawAllow, sawEnable bool
	for _, call := range runner.calls {
		if len(call.Command) >= 2 && call.Command[0] == "ufw" && call.Command[1] == "allow" {
			if sawEnable {
				t.Fatalf("ufw allow ran after enable: %+v", commandsOf(runner))
			}
			sawAllow = true
			continue
		}
		if len(call.Command) >= 3 && call.Command[1] == "--force" && call.Command[2] == "enable" {
			if !sawAllow {
				t.Fatalf("ufw --force enable ran before allow rules: %+v", commandsOf(runner))
			}
			sawEnable = true
		}
	}
	if !sawAllow || !sawEnable {
		t.Fatalf("expected allow then enable, got %+v", commandsOf(runner))
	}
	if !runner.enabled {
		t.Fatal("expected ufw to be enabled after a successful apply")
	}
}

func TestInstallApplyRollsBackEnableWhenRuleApplyFails(t *testing.T) {
	runner := &recordingFirewallRunner{failCommand: "allow"}
	cleanup := setTestUFWApplier(runner)
	defer cleanup()

	dir := t.TempDir()
	paths := ApplyPaths{EtcDir: dir + "/etc/veil", VarDir: dir + "/var/lib/veil"}
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel"}
	plan := InstallPlan{Profile: profile, FirewallActions: testFirewallActions()}

	if _, err := ApplyRURecommendedProfileWithPlan(profile, paths, plan); err == nil {
		t.Fatal("expected firewall apply failure")
	}
	if runner.enabled {
		t.Fatal("failed rule apply left ufw enabled")
	}
	for _, call := range runner.calls {
		if len(call.Command) >= 3 && call.Command[1] == "--force" && call.Command[2] == "enable" {
			t.Fatalf("inactive ufw was enabled after a rule failure: %+v", commandsOf(runner))
		}
	}
}

func TestBuildInstallPlanStagesSSHBeforePanelPort(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "direct",
		Secret:      func(label string) string { return "secret-" + label },
		PanelPort:   2096,
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{
		Platform:     hostenv.Platform{OS: "linux", Arch: "amd64"},
		SystemdUnits: []string{"veil.service"},
		PanelAccess:  profile.PanelAccess,
		PanelPort:    2096,
		SSHPorts:     []int{2222},
	})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if !hasFirewallAction(plan, "2222/tcp") {
		t.Fatalf("install plan missing custom SSH port: %+v", plan.FirewallActions)
	}
	if !hasFirewallAction(plan, "2096/tcp") {
		t.Fatalf("install plan missing panel port: %+v", plan.FirewallActions)
	}
	if len(plan.FirewallActions) < 2 {
		t.Fatalf("expected SSH then panel rules, got %+v", plan.FirewallActions)
	}
	if got := plan.FirewallActions[0].Args[1]; got != "2222/tcp" {
		t.Fatalf("first firewall rule = %s, want 2222/tcp before panel access", got)
	}
}

func commandsOf(runner *recordingFirewallRunner) [][]string {
	out := make([][]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		out = append(out, append([]string(nil), call.Command...))
	}
	return out
}
