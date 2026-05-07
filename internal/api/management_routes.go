package api

import "net/http"

type ManagementRoutes struct{}

func (ManagementRoutes) Paths() []string {
	return []string{
		"/api/settings",
		"/api/inbounds",
		"/api/inbounds/",
		"/api/routing/rules",
		"/api/routing/rules/",
		"/api/routing/presets",
		"/api/routing/presets/",
		"/api/warp",
		"/api/client-links/subscription",
		"/api/client-links",
		"/api/firewall",
		"/api/apply/plan",
		"/api/apply/history",
		"/api/apply",
	}
}

func (r ManagementRoutes) Register(mux *http.ServeMux, s *managementState) {
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/inbounds", s.handleInbounds)
	mux.HandleFunc("/api/inbounds/", s.handleInboundByName)
	mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	mux.HandleFunc("/api/routing/rules/", s.handleRoutingRuleByName)
	mux.HandleFunc("/api/routing/presets", s.handleRoutingPresets)
	mux.HandleFunc("/api/routing/presets/", s.handleRoutingPresetByName)
	mux.HandleFunc("/api/warp", s.handleWarp)
	mux.HandleFunc("/api/client-links/subscription", s.handleClientLinksSubscription)
	mux.HandleFunc("/api/client-links", s.handleClientLinks)
	mux.HandleFunc("/api/firewall", s.handleFirewall)
	mux.HandleFunc("/api/apply/plan", s.handleApplyPlan)
	mux.HandleFunc("/api/apply/history", s.handleApplyHistory)
	mux.HandleFunc("/api/apply", s.handleApply)
}
