package generatedconfig

import "github.com/mikkelchokolate/Veil/internal/renderer"

// RenderWarpRoutingRules converts management routing rules into the sing-box
// rule tags used by the WARP sidecar. The UI uses "proxy" as a generic
// "route through the proxy" outbound; when WARP is the proxy that maps to the
// wireguard endpoint tag "warp". "direct" remains a bypass rule.
func RenderWarpRoutingRules(rules []RoutingRule) []renderer.WarpRoutingRule {
	rendered := []renderer.WarpRoutingRule{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		outbound := rule.Outbound
		if outbound == "proxy" {
			outbound = "warp"
		}
		rendered = append(rendered, renderer.WarpRoutingRule{Match: rule.Match, Outbound: outbound})
	}
	return rendered
}
