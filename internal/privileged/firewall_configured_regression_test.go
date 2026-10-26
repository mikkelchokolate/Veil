package privileged

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestFirewallRollbackKeepsPreStagedOperatorRuleOnInactiveUFW covers the
// reported lockout: the operator stages an SSH allow before enabling UFW, the
// enable fails, and rollback used to delete that rule because `ufw status`
// lists nothing while the firewall is inactive (#1434).
func TestFirewallRollbackKeepsPreStagedOperatorRuleOnInactiveUFW(t *testing.T) {
	initial := map[string]string{"22/tcp": ""}
	model := &transactionalUFWModel{rules: cloneFirewallRules(initial), failAt: 7}
	_, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
	if err == nil {
		t.Fatalf("expected the injected enable failure; calls=%v", model.calls)
	}
	if !reflect.DeepEqual(model.rules, initial) {
		t.Fatalf("rollback changed the operator's configured rules: %v, want %v", model.rules, initial)
	}
}

// TestFirewallReconcileWarnsAboutConfiguredRulesWhileInactive surfaces the
// disabled-firewall state instead of treating it as an empty ruleset (#1434).
func TestFirewallReconcileWarnsAboutConfiguredRulesWhileInactive(t *testing.T) {
	model := &transactionalUFWModel{rules: map[string]string{"22/tcp": ""}}
	result, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	joined := strings.Join(result.Warnings, "\n")
	for _, want := range []string{"UFW is disabled", "1 configured rule(s)", "ufw enable"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings %v do not mention %q", result.Warnings, want)
		}
	}
}

// TestFirewallNoWarningWhenUFWIsActive keeps the normal path quiet.
func TestFirewallNoWarningWhenUFWIsActive(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, rules: map[string]string{"22/tcp": ""}}
	result, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings on an active firewall: %v", result.Warnings)
	}
}

// TestParseUFWStatusWithV6Section keeps the parser stable on the dual-stack
// output real ufw produces (Chief request): the (v6) rows must be recognised
// as ipv6 entries and must not swallow or duplicate the IPv4 rows.
func TestParseUFWStatusWithV6Section(t *testing.T) {
	output := `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere                   # OpenSSH
443/tcp                    ALLOW       Anywhere                   # Panel
22/tcp (v6)                ALLOW       Anywhere (v6)              # OpenSSH
443/tcp (v6)               ALLOW       Anywhere (v6)              # Panel
`
	state, err := parseUFWStatus(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !state.Enabled || len(state.Entries) != 4 {
		t.Fatalf("entries=%d enabled=%v, want 4 entries", len(state.Entries), state.Enabled)
	}
	families := map[string]int{}
	for _, entry := range state.Entries {
		families[entry.Family]++
	}
	if families["ipv4"] != 2 || families["ipv6"] != 2 {
		t.Fatalf("families=%v, want two v4 and two v6 entries", families)
	}
	if _, ok := state.Rules["22/tcp"]; !ok {
		t.Fatalf("IPv4 key missing from rules: %v", state.Rules)
	}
}
