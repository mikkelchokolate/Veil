package api

import "github.com/veil-panel/veil/internal/renderer"

type ManagementConfigInput struct {
	ApplyRoot string
	Settings  Settings
	Inbounds  []Inbound
	Rules     []RoutingRule
	Warp      WarpConfig
}

type ManagementConfigRenderer struct {
	input ManagementConfigInput
}

func NewManagementConfigRenderer(input ManagementConfigInput) ManagementConfigRenderer {
	return ManagementConfigRenderer{input: input}
}

func (r ManagementConfigRenderer) Render() (map[string]string, error) {
	return BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: r.input.ApplyRoot,
		Settings:  r.input.Settings,
		Inbounds:  r.input.Inbounds,
		Rules:     r.input.Rules,
		Warp:      r.input.Warp,
	})
}

func (r ManagementConfigRenderer) HasRenderSettings() bool {
	return hasRenderSettings(r.input.Settings)
}

func (r ManagementConfigRenderer) RenderInbound(inbound Inbound) (string, error) {
	artifact, _, err := NewGeneratedConfigProtocolRegistry().RenderInbound(r.input.Settings, NewGeneratedConfigPaths(r.input.ApplyRoot), inbound)
	return artifact.Body, err
}

func (r ManagementConfigRenderer) RenderWarp() (string, error) {
	warp := r.input.Warp
	setWarpDefaults(&warp)
	return renderer.RenderWarpSingBox(renderer.WarpSingBoxConfig{
		Endpoint:      warp.Endpoint,
		PrivateKey:    warp.PrivateKey,
		LocalAddress:  warp.LocalAddress,
		PeerPublicKey: warp.PeerPublicKey,
		Reserved:      append([]int(nil), warp.Reserved...),
		SocksListen:   warp.SocksListen,
		SocksPort:     warp.SocksPort,
		MTU:           warp.MTU,
		RoutingRules:  RenderWarpRoutingRules(r.input.Rules),
	})
}
