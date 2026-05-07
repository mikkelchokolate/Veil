package api

import "github.com/veil-panel/veil/internal/renderer"

type GeneratedConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

func BuildGeneratedConfigSet(input GeneratedConfigInput) (map[string]string, error) {
	if err := validateGeneratedConfigInboundCardinality(input.Settings, input.Inbounds); err != nil {
		return nil, err
	}
	configs := map[string]string{}
	paths := NewGeneratedConfigPaths(input.ApplyRoot)
	if hasRenderSettings(input.Settings) {
		renderer := NewGeneratedInboundConfigRenderer(input.Settings, paths)
		for _, inbound := range input.Inbounds {
			artifact, ok, err := renderer.Render(inbound)
			if err != nil {
				return nil, err
			}
			if ok {
				configs[artifact.Path] = artifact.Body
			}
		}
	}
	if input.Warp.Enabled {
		warp := input.Warp
		setWarpDefaults(&warp)
		body, err := renderer.RenderWarpSingBox(renderer.WarpSingBoxConfig{
			Endpoint:      warp.Endpoint,
			PrivateKey:    warp.PrivateKey,
			LocalAddress:  warp.LocalAddress,
			PeerPublicKey: warp.PeerPublicKey,
			Reserved:      append([]int(nil), warp.Reserved...),
			SocksListen:   warp.SocksListen,
			SocksPort:     warp.SocksPort,
			MTU:           warp.MTU,
			RoutingRules:  RenderWarpRoutingRules(input.Rules),
		})
		if err != nil {
			return nil, err
		}
		configs[paths.Warp()] = body
	}
	return configs, nil
}
