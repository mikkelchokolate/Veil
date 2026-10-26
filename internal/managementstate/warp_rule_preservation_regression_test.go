package managementstate

import "testing"

// TestMutationDisableWarpKeepsOperatorRules locks #1416: turning WARP off is an
// outbound change, not a purge. Every operator-created warp rule — including
// ones that were already disabled — and the auto rule survive, so enabling WARP
// again restores the previous routing configuration.
func TestMutationDisableWarpKeepsOperatorRules(t *testing.T) {
	rules := []RoutingRule{
		{Name: "warp-routing", Match: "geosite:openai", Outbound: "warp", Enabled: true},
		{Name: "custom-warp", Match: "domain:example.com", Outbound: "warp", Enabled: true},
		{Name: "custom-warp-off", Match: "domain:paused.example.com", Outbound: "warp", Enabled: false},
		{Name: "direct-private", Match: "geoip:private", Outbound: "direct", Enabled: true},
	}
	warp := WarpConfig{Enabled: true}
	m := NewMutation(MutationTarget{Warp: &warp, Rules: &rules}, nil)

	if _, err := m.UpdateWarp(WarpConfig{Enabled: false}); err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("disable deleted routing rules: %+v", rules)
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		seen[rule.Name] = true
	}
	for _, name := range []string{"warp-routing", "custom-warp", "custom-warp-off", "direct-private"} {
		if !seen[name] {
			t.Fatalf("rule %q was deleted: %+v", name, rules)
		}
	}

	// Re-enabling must not duplicate the auto rule or alter the operator's rules.
	if _, err := m.UpdateWarp(WarpConfig{Enabled: true}); err != nil {
		t.Fatalf("UpdateWarp re-enable: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("re-enable changed the rule set: %+v", rules)
	}
	count := 0
	for _, rule := range rules {
		if rule.Outbound == "warp" && rule.Match == "geosite:openai" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("auto warp rule duplicated: %+v", rules)
	}
}
