package hysteria2

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

// RenderConfig generates one Hysteria2 config per enabled inbound.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}
	var artifacts []generatedconfig.GeneratedConfigArtifact
	for _, inbound := range input.Inbounds {
		body, err := renderHysteria2(input.Settings, inbound, input.Warp, input.Rules, input.Paths)
		if err != nil {
			return nil, false, err
		}
		subpath := "hysteria2/" + inbound.Name + ".yaml"
		artifacts = append(artifacts, generatedconfig.GeneratedConfigArtifact{
			Path: input.Paths.Generated(subpath),
			Body: body,
		})
	}
	return artifacts, true, nil
}

// ArtifactSpec returns the artifact metadata for Hysteria2 configs.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.Hysteria2ConfigSubpath,
		ValidationName: "hysteria2",
	}
}

func renderHysteria2(settings model.Settings, inbound model.Inbound, warp model.WarpConfig, rules []model.RoutingRule, paths generatedconfig.Paths) (string, error) {
	password := hysteria2Password(settings, inbound)
	access, err := clientaccess.BuildClientAccess(settings, inbound)
	if err != nil {
		return "", err
	}
	url := masqueradeURL(settings, inbound)
	domain := model.ResolveInboundDomain(inbound, settings)
	hystConfig := renderer.Hysteria2Config{
		ListenPort:         inbound.Port,
		Domain:             domain,
		Password:           password,
		Users:              access.Hysteria2Users(),
		MasqueradeURL:      url,
		TrafficStatsListen: runtimeports.Hysteria2TrafficStatsAddress(inbound.Port),
		TrafficStatsSecret: TrafficStatsSecret(settings, inbound),
	}
	// Use Caddy-managed certificates whenever the inbound has its own domain
	// (Caddy is already required for it) or when the panel itself uses Caddy.
	if domain != "" && (settings.PanelAccess == "caddy" || model.InboundDomain(inbound) != "") {
		hystConfig.CertPath = paths.CertPath(domain)
		hystConfig.KeyPath = paths.KeyPath(domain)
	} else {
		hystConfig.CertPath = paths.PanelCertPath()
		hystConfig.KeyPath = paths.PanelKeyPath()
	}
	if warp.Enabled {
		socksPort := warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		hystConfig.Upstream = "127.0.0.1:" + strconv.Itoa(socksPort)
		hystConfig.GeoIPPath = routingDatPath(paths, "geoip.dat")
		hystConfig.GeoSitePath = routingDatPath(paths, "geosite.dat")
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			hystConfig.RoutingRules = append(hystConfig.RoutingRules, renderer.Hysteria2RoutingRule{
				Match:    rule.Match,
				Outbound: rule.Outbound,
			})
		}
	}
	return renderer.RenderHysteria2(hystConfig)
}

func routingDatPath(paths generatedconfig.Paths, name string) string {
	candidates := []string{paths.Generated("rules/" + name)}
	if paths.LiveRoot != "" {
		candidates = append(candidates, filepath.Join(paths.LiveRoot, "rules", name))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	if paths.LiveRoot != "" {
		return filepath.Join(paths.LiveRoot, "rules", name)
	}
	return candidates[0]
}
