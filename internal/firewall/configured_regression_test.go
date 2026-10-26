package firewall

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestParseUFWShowAddedForms locks the decoder for the configured-ruleset view:
// plain ports, ranges, protocol forms, app profiles and source-restricted rules
// from `ufw show added` (#1434).
func TestParseUFWShowAddedForms(t *testing.T) {
	snap := parseUFWShowAdded(`Added user rules (see 'ufw status' for running firewall):
ufw allow 22/tcp
ufw deny 23/tcp
ufw allow 1000:2000/udp
ufw allow OpenSSH
ufw allow from 1.2.3.4 to any port 80 proto tcp comment 'web'
ufw limit 25/tcp comment 'rate limited'
`)
	for _, tc := range []struct{ target, comment string }{
		{"22/tcp", ""},
		{"23/tcp", ""},
		{"1000:2000/udp", ""},
		{"OpenSSH", ""},
		{"80/tcp", "web"},
		{"25/tcp", "rate limited"},
	} {
		comment, ok := snap.Rules[tc.target]
		if !ok || comment != tc.comment {
			t.Fatalf("rule %q = %q ok=%v, want %q; rules=%v", tc.target, comment, ok, tc.comment, snap.Rules)
		}
	}
	if _, ok := snap.Allows["23/tcp"]; ok {
		t.Fatalf("a deny rule must not count as an allow: %v", snap.Allows)
	}
	if _, ok := snap.Allows["22/tcp"]; !ok {
		t.Fatalf("allow rule missing from the ALLOW subset: %v", snap.Allows)
	}
}

// TestApplySafelyWarnsAndKeepsPreStagedOperatorRuleWhenInactive covers the
// reported failure: the operator staged an SSH allow before enabling UFW, the
// enable failed, and rollback used to delete that rule because `ufw status`
// showed nothing while the firewall was inactive (#1434).
func TestApplySafelyWarnsAndKeepsPreStagedOperatorRuleWhenInactive(t *testing.T) {
	originalWarn := warnInactiveUFW
	var warnings []string
	warnInactiveUFW = func(format string, args ...any) {
		warnings = append(warnings, strings.TrimSpace(fmt.Sprintf(format, args...)))
	}
	t.Cleanup(func() { warnInactiveUFW = originalWarn })

	model := &safeUFWModel{rules: map[string]string{"22/tcp": ""}, failAt: 6}
	rules := []Rule{{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil panel HTTPS"}}}
	if err := NewUFWApplierWithRunner(model).ApplySafely(rules); err == nil {
		t.Fatal("expected the injected enable failure")
	}
	if _, ok := model.rules["22/tcp"]; !ok {
		t.Fatalf("rollback deleted the operator's pre-staged SSH allow: %v", model.rules)
	}
	if !reflect.DeepEqual(model.rules, map[string]string{"22/tcp": ""}) {
		t.Fatalf("rollback left unexpected rules: %v", model.rules)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, "\n"), "UFW is disabled but 1 configured rule(s) exist") {
		t.Fatalf("operator rules were not surfaced as a warning: %v", warnings)
	}
}

// TestPruneStaleManagedRulesSeesConfiguredRulesWhenInactive keeps stale Veil
// rules from going live on the next enable (#1434).
func TestPruneStaleManagedRulesSeesConfiguredRulesWhenInactive(t *testing.T) {
	model := &safeUFWModel{rules: map[string]string{"2096/tcp": "Veil panel", "22/tcp": "Veil management SSH"}}
	pruned, err := NewUFWApplierWithRunner(model).PruneStaleManagedRules([]Rule{
		{Command: "ufw", Args: []string{"allow", "22/tcp", "comment", "Veil management SSH"}},
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned=%d, want the stale 2096/tcp rule", pruned)
	}
	if _, ok := model.rules["2096/tcp"]; ok {
		t.Fatalf("stale Veil rule survived an inactive firewall: %v", model.rules)
	}
	if _, ok := model.rules["22/tcp"]; !ok {
		t.Fatalf("management SSH rule was pruned: %v", model.rules)
	}
}

// TestRemoveManagedRulesSeesConfiguredRulesWhenInactive covers uninstall.
func TestRemoveManagedRulesSeesConfiguredRulesWhenInactive(t *testing.T) {
	model := &safeUFWModel{rules: map[string]string{"2096/tcp": "Veil panel", "22/tcp": "OpenSSH"}}
	removed, err := NewUFWApplierWithRunner(model).RemoveManagedRules()
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(removed) != 1 || removed[0] != "2096/tcp" {
		t.Fatalf("removed=%v, want the Veil rule only", removed)
	}
	if _, ok := model.rules["22/tcp"]; !ok {
		t.Fatalf("operator rule was removed: %v", model.rules)
	}
}
