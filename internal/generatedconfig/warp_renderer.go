package generatedconfig

import (
	"github.com/veil-panel/veil/internal/renderer"
	veilwarp "github.com/veil-panel/veil/internal/warp"
)

type GeneratedWarpConfigRenderer struct {
	paths GeneratedConfigPaths
}

func NewGeneratedWarpConfigRenderer(paths GeneratedConfigPaths) GeneratedWarpConfigRenderer {
	return GeneratedWarpConfigRenderer{paths: paths}
}

func (r GeneratedWarpConfigRenderer) Render(warp WarpConfig, rules []RoutingRule) (GeneratedConfigArtifact, bool, error) {
	if !warp.Enabled {
		return GeneratedConfigArtifact{}, false, nil
	}
	veilwarp.SetDefaults(&warp)
	body, err := renderer.RenderWarpSingBox(renderer.WarpSingBoxConfig{
		Endpoint:      warp.Endpoint,
		PrivateKey:    warp.PrivateKey,
		LocalAddress:  warp.LocalAddress,
		PeerPublicKey: warp.PeerPublicKey,
		Reserved:      append([]int(nil), warp.Reserved...),
		SocksListen:   warp.SocksListen,
		SocksPort:     warp.SocksPort,
		MTU:           warp.MTU,
		RoutingRules:  RenderWarpRoutingRules(rules),
	})
	if err != nil {
		return GeneratedConfigArtifact{}, true, err
	}
	return GeneratedConfigArtifact{Path: r.paths.Warp(), Body: body}, true, nil
}
