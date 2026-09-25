package managementstate

import "testing"

// TestUpdateWarpEnableAvoidsDuplicateWarpRoutingName covers #1004: enabling
// WARP auto-prepends a "warp-routing" rule; when the operator already owns a
// rule by that name (pointing at another outbound — a legal name via
// CreateRoutingRule), the auto-rule must pick a collision-free name instead
// of persisting a duplicate that breaks snapshot validation and shadows
// name-addressed updates/deletes.
func TestUpdateWarpEnableAvoidsDuplicateWarpRoutingName(t *testing.T) {
	warp := WarpConfig{Enabled: false}
	rules := []RoutingRule{
		{Name: "warp-routing", Match: "domain:example.com", Outbound: "direct", Enabled: true},
	}
	mutation := NewMutation(MutationTarget{Warp: &warp, Rules: &rules}, nil)

	if _, err := mutation.UpdateWarp(WarpConfig{Enabled: true}); err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	seen := map[string]int{}
	for _, r := range rules {
		seen[r.Name]++
	}
	if seen["warp-routing"] != 1 {
		t.Fatalf("duplicate rule name persisted: %+v", rules)
	}
	// The auto-inserted rule (outbound warp, geosite:openai match) must still
	// be present under the collision-free name.
	found := false
	for _, r := range rules {
		if r.Outbound == "warp" && r.Match == "geosite:openai" && r.Enabled {
			found = true
			if r.Name == "warp-routing" {
				t.Fatalf("auto rule reused the taken name: %+v", rules)
			}
		}
	}
	if !found {
		t.Fatalf("auto WARP rule missing: %+v", rules)
	}
}

// TestUpdateWarpEnableKeepsCanonicalNameWithoutCollision locks the unchanged
// contract: absent a collision the auto rule keeps the "warp-routing" name.
func TestUpdateWarpEnableKeepsCanonicalNameWithoutCollision(t *testing.T) {
	warp := WarpConfig{Enabled: false}
	rules := []RoutingRule{
		{Name: "other", Match: "domain:example.com", Outbound: "direct", Enabled: true},
	}
	mutation := NewMutation(MutationTarget{Warp: &warp, Rules: &rules}, nil)

	if _, err := mutation.UpdateWarp(WarpConfig{Enabled: true}); err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	if len(rules) != 2 || rules[0].Name != "warp-routing" || rules[0].Outbound != "warp" {
		t.Fatalf("warp rule = %+v", rules)
	}
}
