package applyplan

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/routing"
)

// #1410: the write path validates outbound against routing.Outbounds, so the
// planner must accept exactly that set — otherwise one side drifts and a rule
// either saves fine but never applies, or is refused at save time although
// the planner would accept it.
func TestPlannerAcceptsExactlyRoutingOutbounds(t *testing.T) {
	for _, outbound := range routing.Outbounds {
		plan := Build(Input{
			Rules: []model.RoutingRule{{Name: "r", Match: "geoip:private", Outbound: outbound, Enabled: true}},
			Warp:  model.WarpConfig{Enabled: true},
		})
		for _, e := range plan.Errors {
			if strings.Contains(e, "unsupported routing outbound") {
				t.Fatalf("planner rejects routing.Outbounds member %q: %v", outbound, plan.Errors)
			}
		}
	}
	for _, outbound := range []string{"Direct", "WARP", " proxy", "block"} {
		if routing.IsValidOutbound(outbound) {
			t.Fatalf("routing.IsValidOutbound(%q) = true", outbound)
		}
		plan := Build(Input{
			Rules: []model.RoutingRule{{Name: "r", Match: "geoip:private", Outbound: outbound, Enabled: true}},
			Warp:  model.WarpConfig{Enabled: true},
		})
		if !strings.Contains(strings.Join(plan.Errors, ";"), "unsupported routing outbound: "+outbound) {
			t.Fatalf("planner accepts %q that the write path rejects: %v", outbound, plan.Errors)
		}
	}
}
