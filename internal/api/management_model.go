package api

type managementModel struct {
	Settings Settings
	Inbounds []Inbound
	Rules    []RoutingRule
	Warp     WarpConfig
}

func defaultManagementModel(mode string) managementModel {
	return managementModel{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: mode},
		Inbounds: []Inbound{},
		Rules: []RoutingRule{
			{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true},
		},
		Warp: WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}
}
