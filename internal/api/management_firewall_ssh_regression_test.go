package api

import (
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func stubDetectSSHPorts(t *testing.T, ports []int) {
	t.Helper()
	orig := detectSSHPorts
	detectSSHPorts = func() []int { return append([]int(nil), ports...) }
	t.Cleanup(func() { detectSSHPorts = orig })
}

// #629: the local apply path must stage the detected SSH management ports
// with the same "Veil management SSH" comment install uses — before this fix
// a panel apply built rules only from BuildRuleResponses (panel / inbounds /
// ACME), so ApplySafely had no SSH evidence and a first enable could strand
// the operator's management channel.
func TestSyncFirewallStagesDetectedSSHManagementRules(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.settings.PanelListen = "0.0.0.0:3000"
	state.settings.PanelAccess = "direct"
	stubDetectSSHPorts(t, []int{2222})

	fake := &fakeFirewallApplier{}
	old := currentFirewallApplier()
	swapFirewallApplier(fake)
	t.Cleanup(func() { swapFirewallApplier(old) })

	ctx := NewManagementApplyContext(state)
	results := ctx.syncFirewall()
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("expected one successful firewall sync, got %+v", results)
	}
	if len(fake.gotRules) < 2 {
		t.Fatalf("expected SSH + panel rules, got %+v", fake.gotRules)
	}
	wantFirst := []string{"allow", "2222/tcp", "comment", "Veil management SSH"}
	if !reflect.DeepEqual(fake.gotRules[0].Args, wantFirst) {
		t.Fatalf("first staged rule = %v, want SSH management allow %v", fake.gotRules[0].Args, wantFirst)
	}
	foundPanel := false
	for _, rule := range fake.gotRules[1:] {
		if len(rule.Args) >= 2 && rule.Args[0] == "allow" && rule.Args[1] == "3000/tcp" {
			foundPanel = true
		}
	}
	if !foundPanel {
		t.Fatalf("panel rule missing after SSH staging: %+v", fake.gotRules)
	}
}

// #629: the privileged prepare path sends the desired set to the helper —
// whose enable gate accepts only SSH evidence — so the SSH rules must be in
// that payload, not just in the local ApplySafely path.
func TestPrepareFirewallLockedStagesSSHManagementRules(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})
	state.settings.PanelListen = "0.0.0.0:2096"
	state.settings.PanelAccess = "direct"
	stubDetectSSHPorts(t, []int{2222})
	client := &recordingPrivilegedClient{
		firewallResult: privileged.FirewallResult{Prepared: true, TransactionID: "tx-ssh"},
	}
	state.privileged = client
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	txID, err := ctx.PrepareFirewallLocked()
	if err != nil {
		t.Fatalf("PrepareFirewallLocked: %v", err)
	}
	if txID != "tx-ssh" {
		t.Fatalf("transaction id = %q, want tx-ssh", txID)
	}
	if len(client.firewallRequests) != 1 {
		t.Fatalf("expected one firewall prepare, got %+v", client.firewallRequests)
	}
	rules := client.firewallRequests[0].Rules
	if len(rules) < 2 {
		t.Fatalf("expected SSH + panel desired rules, got %+v", rules)
	}
	wantFirst := []string{"allow", "2222/tcp", "comment", "Veil management SSH"}
	if !reflect.DeepEqual(rules[0].Args, wantFirst) {
		t.Fatalf("first desired rule = %v, want %v", rules[0].Args, wantFirst)
	}
}

// #629: an inbound already occupying a detected SSH port must not emit a
// duplicate target — the privileged parser rejects duplicate targets — so
// the SSH staging wins and keeps the management comment the gates look for.
func TestDesiredFirewallUFWRulesDedupesSSHPortCollision(t *testing.T) {
	stubDetectSSHPorts(t, []int{443})
	rules := desiredFirewallUFWRules(Settings{PanelAccess: "caddy", PanelPublicPort: 443}, nil)
	if len(rules) != 1 {
		t.Fatalf("rules = %+v, want a single SSH management rule", rules)
	}
	want := []string{"allow", "443/tcp", "comment", "Veil management SSH"}
	if !reflect.DeepEqual(rules[0].Args, want) {
		t.Fatalf("rule = %v, want %v", rules[0].Args, want)
	}
}

// #629 + #356: when no service rules are produced the desired set stays
// empty — SSH is not unioned in. The privileged reconcile still runs to
// prune stale Veil-managed rules, and an empty desired set can never enable
// UFW (mirroring install's UFWPlan, which stages SSH only alongside real
// openings).
func TestDesiredFirewallUFWRulesStaysEmptyWithoutServiceRules(t *testing.T) {
	stubDetectSSHPorts(t, []int{2222})
	settings := Settings{PanelAccess: "local", PanelListen: "127.0.0.1:2096"}
	if rules := desiredFirewallUFWRules(settings, nil); len(rules) != 0 {
		t.Fatalf("loopback-only panel should produce an empty desired set, got %+v", rules)
	}
}
