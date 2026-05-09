package managementstate

import "github.com/veil-panel/veil/internal/model"

type DefaultInput struct {
	PanelListen string
	PanelAccess string
	WebBasePath string
	Mode        string
	Domain      string
	Email       string
}

type DefaultState struct {
	Settings model.Settings
	Inbounds []model.Inbound
	Rules    []model.RoutingRule
	Warp     model.WarpConfig
}

func BuildDefaultState(input DefaultInput) DefaultState {
	panelListen := input.PanelListen
	if panelListen == "" {
		panelListen = "127.0.0.1:2096"
	}
	panelAccess := input.PanelAccess
	webBasePath := input.WebBasePath
	if webBasePath == "/" {
		webBasePath = ""
	}
	if panelAccess == "" && webBasePath != "" {
		panelAccess = "caddy"
	}
	return DefaultState{
		Settings: model.Settings{PanelListen: panelListen, PanelAccess: panelAccess, WebBasePath: webBasePath, Mode: input.Mode, Domain: input.Domain, Email: input.Email},
		Inbounds: []model.Inbound{},
		Rules: []model.RoutingRule{
			{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true},
		},
		Warp: model.WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}
}
