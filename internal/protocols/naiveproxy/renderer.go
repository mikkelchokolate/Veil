package naiveproxy

import (
	"fmt"
	"net"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// RenderConfig generates the consolidated Caddy JSON config for all naiveproxy
// inbounds (and panel access when panelAccess is caddy). The inbound/Caddy
// redesign moves from per-inbound Caddyfiles to a single JSON config used by
// veil-caddy.service.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}

	// The configured etc root (derived from the render paths' live root) is
	// authoritative here: a custom --etc-dir install must validate and serve
	// its own <etc>/www fallback tree, not the packaged /etc/veil one
	// (issue #634). An empty EtcRoot falls back to the host environment.
	plan, _, _, err := caddyassembly.BuildFinalRenderPlanForEtcDir(input.Settings, input.Inbounds, input.Paths.EtcRoot)
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
				owner.Upstream = "socks5://" + net.JoinHostPort(input.Warp.SocksDialAddr(), strconv.Itoa(socksPort))
				plan.Servers[key] = owner
			}
		}
	}

	caps, err := caddycapabilities.Probe("")
	if err != nil {
		if !caddycapabilities.IsMissingBinary(err) {
			return nil, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
		}
		// A Hysteria2-only plan (audit #156) renders this artifact before the
		// Caddy runtime exists; tolerate a missing binary the way the
		// panelaccess path does. Naive inbounds still fail hard: they need the
		// forward_proxy module probed.
		for _, inb := range input.Inbounds {
			if inb.Protocol == "naiveproxy" && inb.Enabled {
				return nil, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
			}
		}
		caps = caddycapabilities.CaddyCapabilities{}
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

// ArtifactSpec returns the artifact metadata for the consolidated Caddy JSON
// config.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.CaddyJSONConfigSubpath,
		ValidationName: "caddy",
		ValidationCommand: func(path string) []string {
			return []string{"caddy", "validate", "--config", path}
		},
	}
}
