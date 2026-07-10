package api

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	veilwarp "github.com/mikkelchokolate/Veil/internal/warp"
)

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
	return generatedconfig.NewGeneratedRenderSettingsPolicy().HasRenderSettings(r.input.Settings, r.input.Inbounds)
}

func (r ManagementConfigRenderer) RenderInbound(inbound Inbound) (string, error) {
	artifact, _, err := protocols.NewGeneratedConfigRegistry().RenderInbound(r.input.Settings, generatedconfig.NewPaths(r.input.ApplyRoot), inbound, r.input.Warp)
	return artifact.Body, err
}

func (r ManagementConfigRenderer) RenderWarp() (string, error) {
	warp := r.input.Warp
	veilwarp.SetDefaults(&warp)
	return renderer.RenderWarpSingBox(renderer.WarpSingBoxConfig{
		Endpoint:      warp.Endpoint,
		PrivateKey:    warp.PrivateKey,
		LocalAddress:  warp.LocalAddress,
		PeerPublicKey: warp.PeerPublicKey,
		Reserved:      append([]int(nil), warp.Reserved...),
		SocksListen:   warp.SocksListen,
		SocksPort:     warp.SocksPort,
		MTU:           warp.MTU,
		RoutingRules:  generatedconfig.RenderWarpRoutingRules(r.input.Rules),
	})
}
