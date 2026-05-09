package api

import (
	"fmt"
	"net"
	"strconv"

	"github.com/veil-panel/veil/internal/renderer"
)

type PanelAccess struct {
	settings Settings
}

type PanelAccessApplyIntent struct {
	Configs  []string
	Actions  []string
	Runtimes []string
	Errors   []string
}

func NewPanelAccess(settings Settings) PanelAccess {
	return PanelAccess{settings: settings}
}

func (p PanelAccess) CaddyRoute() (PanelCaddyRoute, bool, error) {
	settings := p.settings
	if settings.PanelAccess != "caddy" {
		return PanelCaddyRoute{}, false, nil
	}
	webBasePath := normalizeSettingsWebBasePath(settings.WebBasePath)
	if webBasePath == "" {
		return PanelCaddyRoute{}, false, fmt.Errorf("webBasePath is required for caddy Panel access")
	}
	_, portText, err := net.SplitHostPort(settings.PanelListen)
	if err != nil {
		return PanelCaddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return PanelCaddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	return PanelCaddyRoute{Port: port, WebBasePath: webBasePath}, true, nil
}

func (p PanelAccess) GeneratedConfig(paths GeneratedConfigPaths) (GeneratedConfigArtifact, bool, error) {
	route, ok, err := p.CaddyRoute()
	if err != nil || !ok {
		return GeneratedConfigArtifact{}, ok, err
	}
	body, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: p.settings.Domain, Email: p.settings.Email, PanelPort: route.Port, WebBasePath: route.WebBasePath})
	if err != nil {
		return GeneratedConfigArtifact{}, false, err
	}
	return GeneratedConfigArtifact{Path: paths.Caddyfile(), Body: body}, true, nil
}

func (p PanelAccess) ApplyIntent(inbounds []Inbound) PanelAccessApplyIntent {
	intent := PanelAccessApplyIntent{}
	if p.settings.PanelAccess != "caddy" {
		return intent
	}
	if p.settings.Domain == "" || p.settings.Email == "" {
		intent.Errors = append(intent.Errors, "--domain and --email are required for caddy Panel access")
	} else if _, _, err := p.CaddyRoute(); err != nil {
		intent.Errors = append(intent.Errors, err.Error())
	} else {
		intent.Configs = append(intent.Configs, "/etc/veil/generated/caddy/Caddyfile")
		intent.Actions = append(intent.Actions, "reload "+renderer.UnitNaive)
		intent.Runtimes = append(intent.Runtimes, renderer.UnitNaive)
	}
	protocols := NewInboundProtocolCatalog()
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Transport == "tcp" && inbound.Port == 443 && !protocols.RequiresCaddy(inbound.Protocol) {
			intent.Errors = append(intent.Errors, "panel caddy access uses 443/tcp; choose another TCP port for inbound "+inbound.Name)
		}
	}
	return intent
}
