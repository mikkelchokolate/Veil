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
		Inbounds: []Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "hysteria2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		},
		Rules: []RoutingRule{
			{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true},
		},
		Warp: WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}
}
