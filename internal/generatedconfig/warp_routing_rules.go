package generatedconfig

import "github.com/mikkelchokolate/Veil/internal/renderer"

// RenderWarpRoutingRules converts management routing rules into the sing-box
// rule tags used by the WARP sidecar. direct exits locally (bypass proxy and
// WARP). warp uses the WireGuard endpoint. proxy uses a separate local-exit
// tag and must not be rewritten to warp.
func RenderWarpRoutingRules(rules []RoutingRule) []renderer.WarpRoutingRule {
	rendered := []renderer.WarpRoutingRule{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		rendered = append(rendered, renderer.WarpRoutingRule{Match: rule.Match, Outbound: rule.Outbound})
	}
	return rendered
}
