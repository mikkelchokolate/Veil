package panelaccess

import (
	"fmt"
	"net"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
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

func panelDomain(settings model.Settings) string {
	if settings.PanelDomain != "" {
		return settings.PanelDomain
	}
	return settings.Domain
}

func panelEmail(settings model.Settings) string {
	if settings.PanelEmail != "" {
		return settings.PanelEmail
	}
	return settings.Email
}

func (p PanelAccess) GeneratedConfig(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
	if p.settings.PanelAccess != "caddy" {
		return generatedconfig.GeneratedConfigArtifact{}, false, nil
	}
	if panelDomain(p.settings) == "" {
		return generatedconfig.GeneratedConfigArtifact{}, false, fmt.Errorf("domain is required")
	}
	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(p.settings, nil)
	if err != nil {
		return generatedconfig.GeneratedConfigArtifact{}, false, err
	}
	caps, err := caddycapabilities.Probe("")
	if err != nil {
		return generatedconfig.GeneratedConfigArtifact{}, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
	}
	body, err := renderer.RenderCaddyJSON(plan, caps)
	if err != nil {
		return generatedconfig.GeneratedConfigArtifact{}, false, err
	}
	return generatedconfig.GeneratedConfigArtifact{Path: paths.CaddyJSON(), Body: string(body)}, true, nil
}

func (p PanelAccess) ApplyIntent(inbounds []model.Inbound) ApplyIntent {
	intent := ApplyIntent{}
	if p.settings.PanelAccess != "caddy" {
		return intent
	}
	if panelDomain(p.settings) == "" || panelEmail(p.settings) == "" {
		intent.Errors = append(intent.Errors, "--domain and --email are required for caddy Panel access")
	} else if _, _, err := p.CaddyRoute(); err != nil {
		intent.Errors = append(intent.Errors, err.Error())
	} else {
		intent.Configs = append(intent.Configs, "/etc/veil/generated/caddy/config.json")
		intent.Actions = append(intent.Actions, "reload veil-caddy.service")
		intent.Runtimes = append(intent.Runtimes, "veil-caddy.service")
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
