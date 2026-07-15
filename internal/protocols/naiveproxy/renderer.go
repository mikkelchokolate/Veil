package naiveproxy

import (
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// RenderConfig generates the consolidated Caddy JSON config for every
// Caddy-managed endpoint. ProtocolRegistry supplies the selected naiveproxy
// inbounds in Inbounds and the complete enabled inbound set in AllInbounds so
// domain-only hysteria2 certificate owners are not lost when naiveproxy exists.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	inbounds := input.AllInbounds
	if inbounds == nil {
		inbounds = append([]model.Inbound(nil), input.Inbounds...)
		// ProtocolRenderInput.Inbounds is already the selected protocol set. Direct
		// renderer callers historically omitted Enabled, so treat this selected set
		// as active without changing the all-inbounds production path.
		for i := range inbounds {
			inbounds[i].Enabled = true
		}
	}
	if len(inbounds) == 0 && input.Settings.PanelAccess != "caddy" {
		return nil, false, nil
	}

	renderSettings := input.Settings
	if renderSettings.DefaultAcmeEmail == "" && renderSettings.PanelEmail == "" {
		renderSettings.DefaultAcmeEmail = renderSettings.Email
	}
	if renderSettings.PanelAccess == "caddy" {
		domain := renderSettings.PanelDomain
		if domain == "" {
			domain = renderSettings.Domain
		}
		email := renderSettings.PanelEmail
		if email == "" {
			email = renderSettings.Email
		}
		if domain == "" || email == "" {
			return nil, false, fmt.Errorf("panel domain and email are required for caddy Panel access")
		}
	}
	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(renderSettings, inbounds)
	if err != nil {
		return nil, false, err
	}
	if input.Warp.Enabled {
		socksPort := input.Warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		for key, owner := range plan.Servers {
			if owner.Kind == caddyassembly.CaddyOwnerNaive {
				owner.Upstream = fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
				plan.Servers[key] = owner
			}
		}
	}

	caps, err := caddycapabilities.Probe("")
	if err != nil {
		return nil, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
	}
	data, err := renderer.RenderCaddyJSON(plan, caps)
	if err != nil {
		return nil, false, err
	}

	return []generatedconfig.GeneratedConfigArtifact{{
		Path: input.Paths.CaddyJSON(),
		Body: string(data),
	}}, true, nil
}

func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.CaddyJSONConfigSubpath,
		ValidationName: "caddy",
		ValidationCommand: func(path string) []string {
			return []string{"caddy", "validate", "--config", path}
		},
	}
}
