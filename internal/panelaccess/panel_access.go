package panelaccess

import (
	"fmt"
	"net"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

type RequiresCaddyFunc func(protocol string) bool

type PanelAccess struct {
	settings      model.Settings
	requiresCaddy RequiresCaddyFunc
}

type CaddyRoute struct {
	Port        int
	WebBasePath string
}

type ApplyIntent struct {
	Configs  []string
	Actions  []string
	Runtimes []string
	Errors   []string
}

func New(settings model.Settings, requiresCaddy RequiresCaddyFunc) PanelAccess {
	return PanelAccess{settings: settings, requiresCaddy: requiresCaddy}
}

func (p PanelAccess) CaddyRoute() (CaddyRoute, bool, error) {
	settings := p.settings
	if settings.PanelAccess != "caddy" {
		return CaddyRoute{}, false, nil
	}
	webBasePath := veilsettings.NormalizeWebBasePath(settings.WebBasePath)
	if webBasePath == "" {
		return CaddyRoute{}, false, fmt.Errorf("webBasePath is required for caddy Panel access")
	}
	_, portText, err := net.SplitHostPort(settings.PanelListen)
	if err != nil {
		return CaddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return CaddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	return CaddyRoute{Port: port, WebBasePath: webBasePath}, true, nil
}

func (p PanelAccess) GeneratedConfig(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
	route, ok, err := p.CaddyRoute()
	if err != nil || !ok {
		return generatedconfig.GeneratedConfigArtifact{}, ok, err
	}
	body, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: p.settings.Domain, Email: p.settings.Email, PanelPort: route.Port, WebBasePath: route.WebBasePath})
	if err != nil {
		return generatedconfig.GeneratedConfigArtifact{}, false, err
	}
	return generatedconfig.GeneratedConfigArtifact{Path: paths.Caddyfile(), Body: body}, true, nil
}

func (p PanelAccess) ApplyIntent(inbounds []model.Inbound) ApplyIntent {
	intent := ApplyIntent{}
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
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Transport == "tcp" && inbound.Port == 443 && !p.protocolRequiresCaddy(inbound.Protocol) {
			intent.Errors = append(intent.Errors, "panel caddy access uses 443/tcp; choose another TCP port for inbound "+inbound.Name)
		}
	}
	return intent
}

func (p PanelAccess) protocolRequiresCaddy(protocol string) bool {
	return p.requiresCaddy != nil && p.requiresCaddy(protocol)
}
