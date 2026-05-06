package api

import "testing"

func TestRoutingRuleManagementCreateMutatesAndSaves(t *testing.T) {
	rules := []RoutingRule{}
	saves := 0
	management := NewRoutingRuleManagement(&rules, func() error {
		saves++
		return nil
	})

	created, err := management.Create(RoutingRule{Name: "non-ru", Match: "geosite:geolocation-!ru", Outbound: "warp", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "non-ru" || len(rules) != 1 {
		t.Fatalf("unexpected create result: created=%+v rules=%+v", created, rules)
	}
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}
}

func TestRoutingRuleManagementDoesNotSaveOnDuplicateName(t *testing.T) {
	rules := []RoutingRule{{Name: "non-ru", Match: "all", Outbound: "direct"}}
	saves := 0
	management := NewRoutingRuleManagement(&rules, func() error {
		saves++
		return nil
	})

	_, err := management.Create(RoutingRule{Name: "non-ru", Match: "geoip:ru", Outbound: "direct"})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
	if saves != 0 {
		t.Fatalf("saves = %d, want 0", saves)
	}
}
