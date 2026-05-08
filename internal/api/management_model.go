package api

type managementModel struct {
	Settings Settings
	Inbounds []Inbound
	Rules    []RoutingRule
	Warp     WarpConfig
}

func defaultManagementModel(info ServerInfo) managementModel {
	panelListen := info.PanelListen
	if panelListen == "" {
		panelListen = "127.0.0.1:2096"
	}
	panelAccess := info.PanelAccess
	webBasePath := info.WebBasePath
	if webBasePath == "/" {
		webBasePath = ""
	}
	if panelAccess == "" && webBasePath != "" {
		panelAccess = "caddy"
	}
	return managementModel{
		Settings: Settings{PanelListen: panelListen, PanelAccess: panelAccess, WebBasePath: webBasePath, Stack: "panel", Mode: info.Mode, Domain: info.Domain, Email: info.Email},
		Inbounds: []Inbound{},
		Rules: []RoutingRule{
			{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true},
		},
		Warp: WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}
}
