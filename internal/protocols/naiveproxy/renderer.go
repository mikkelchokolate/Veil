package naiveproxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// RenderConfig generates per-inbound Caddyfiles and a standalone panel Caddyfile.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		if input.Settings.PanelAccess == "caddy" {
			body, err := renderPanelStandalone(input.Settings)
			if err != nil {
				return nil, false, err
			}
			if body != "" {
				return []generatedconfig.GeneratedConfigArtifact{{Path: input.Paths.Generated("caddy/panel.Caddyfile"), Body: body}}, true, nil
			}
		}
		return nil, false, nil
	}

	hasInboundOn443 := false
	for _, inbound := range input.Inbounds {
		if inbound.Port == 443 {
			hasInboundOn443 = true
			break
		}
	}

	var artifacts []generatedconfig.GeneratedConfigArtifact
	for i, inbound := range input.Inbounds {
		includePanel := false
		if input.Settings.PanelAccess == "caddy" {
			if inbound.Port == 443 || (!hasInboundOn443 && i == 0) {
				includePanel = true
			}
		}
		body, err := renderNaive(input.Settings, inbound, input.Warp, includePanel)
		if err != nil {
			return nil, false, err
		}
		subpath := "caddy/" + inbound.Name + ".Caddyfile"
		artifacts = append(artifacts, generatedconfig.GeneratedConfigArtifact{
			Path: input.Paths.Generated(subpath),
			Body: body,
		})
	}
	return artifacts, true, nil
}

// ArtifactSpec returns the artifact metadata for naiveproxy configs.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.CaddyfileSubpath,
		ValidationName: "caddy",
		ValidationCommand: func(path string) []string {
			return []string{"caddy", "validate", "--config", path}
		},
	}
}

func renderNaive(settings model.Settings, inbound model.Inbound, warp model.WarpConfig, includePanel bool) (string, error) {
	var buf strings.Builder
	buf.WriteString("{\n  order forward_proxy before file_server\n  servers {\n    protocols h1 h2\n  }\n}\n\n")

	password := naivePassword(settings, inbound)
	access, err := clientaccess.BuildClientAccess(settings, inbound)
	if err != nil {
		return "", err
	}
	username := naiveUsername(settings, inbound)
	root := fallbackRoot(settings, inbound)

	naiveConfig := renderer.NaiveConfig{
		Domain:       settings.Domain,
		Email:        settings.Email,
		ListenPort:   inbound.Port,
		Username:     username,
		Password:     password,
		Users:        access.NaiveUsers(),
		FallbackRoot: root,
	}
	if warp.Enabled {
		socksPort := warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		naiveConfig.Upstream = fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
	}
	if includePanel && settings.PanelAccess == "caddy" {
		route, ok, err := panelCaddyRoute(settings)
		if err != nil {
			return "", err
		}
		if ok {
			naiveConfig.PanelPort = route.Port
			naiveConfig.WebBasePath = route.WebBasePath
		}
	}
	block, err := renderer.RenderNaiveCaddyfile(naiveConfig)
	if err != nil {
		return "", err
	}
	if idx := strings.Index(block, "\n:"); idx != -1 {
		block = block[idx+1:]
	} else if idx := strings.Index(block, "\n"+settings.Domain); idx != -1 {
		block = block[idx+1:]
	}
	buf.WriteString(block)
	buf.WriteString("\n")
	return buf.String(), nil
}

func renderPanelStandalone(settings model.Settings) (string, error) {
	if settings.PanelAccess != "caddy" {
		return "", nil
	}
	route, ok, err := panelCaddyRoute(settings)
	if err != nil || !ok {
		return "", err
	}
	var buf strings.Builder
	buf.WriteString("{\n  order forward_proxy before file_server\n  servers {\n    protocols h1 h2\n  }\n}\n\n")

	panelBlock, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{
		Domain:      settings.Domain,
		Email:       settings.Email,
		PanelPort:   route.Port,
		WebBasePath: route.WebBasePath,
	})
	if err != nil {
		return "", err
	}
	buf.WriteString(panelBlock)
	buf.WriteString("\n")
	return buf.String(), nil
}

type caddyRoute struct {
	Port        int
	WebBasePath string
}

func panelCaddyRoute(settings model.Settings) (caddyRoute, bool, error) {
	if settings.PanelAccess != "caddy" {
		return caddyRoute{}, false, nil
	}
	webBasePath := veilsettings.NormalizeWebBasePath(settings.WebBasePath)
	if webBasePath == "" {
		return caddyRoute{}, false, fmt.Errorf("webBasePath is required for caddy Panel access")
	}
	_, portText, err := net.SplitHostPort(settings.PanelListen)
	if err != nil {
		return caddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return caddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	return caddyRoute{Port: port, WebBasePath: webBasePath}, true, nil
}
