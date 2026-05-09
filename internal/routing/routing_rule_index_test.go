package routing

import "testing"

func TestRoutingRuleIndexFindsRuleByName(t *testing.T) {
	index := NewRoutingRuleIndex([]RoutingRule{{Name: "direct"}, {Name: "warp"}})
	if got := index.Index("warp"); got != 1 {
		t.Fatalf("index = %d", got)
	}
	if index.Index("missing") != -1 {
		t.Fatal("expected missing index")
	}
	if !index.Has("direct") || index.Has("missing") {
		t.Fatalf("unexpected Has result")
	}
}
