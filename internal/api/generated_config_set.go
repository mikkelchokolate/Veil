package api

import (
	"fmt"
	"path/filepath"

	"github.com/veil-panel/veil/internal/renderer"
)

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
	if hasRenderSettings(input.Settings) {
		for _, inbound := range input.Inbounds {
			if !inbound.Enabled || !stackIncludesProtocol(input.Settings.Stack, inbound.Protocol) {
				continue
			}
			switch inbound.Protocol {
			case "naiveproxy":
				body, err := renderNaiveGeneratedConfig(input.Settings, inbound)
				if err != nil {
					return nil, err
				}
				configs[filepath.Join(input.ApplyRoot, "generated", "caddy", "Caddyfile")] = body
			case "hysteria2":
				body, err := renderHysteria2GeneratedConfig(input.Settings, inbound)
				if err != nil {
					return nil, err
				}
				configs[filepath.Join(input.ApplyRoot, "generated", "hysteria2", "server.yaml")] = body
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
			RoutingRules:  renderWarpRoutingRules(input.Rules),
		})
		if err != nil {
			return nil, err
		}
		configs[filepath.Join(input.ApplyRoot, "generated", "sing-box", "warp.json")] = body
	}
	return configs, nil
}

func validateGeneratedConfigInboundCardinality(settings Settings, inbounds []Inbound) error {
	counts := map[string]int{}
	for _, inbound := range inbounds {
		if !inbound.Enabled || !stackIncludesProtocol(settings.Stack, inbound.Protocol) {
			continue
		}
		switch inbound.Protocol {
		case "naiveproxy", "hysteria2":
			counts[inbound.Protocol]++
		}
	}
	for protocol, count := range counts {
		if count > 1 {
			return fmt.Errorf("multiple enabled %s inbounds are not renderable as a single generated config yet", protocol)
		}
	}
	return nil
}

func hasRenderSettings(settings Settings) bool {
	return settings.Domain != "" || settings.Email != "" || settings.NaiveUsername != "" || settings.NaivePassword != "" || settings.Hysteria2Password != "" || settings.MasqueradeURL != "" || settings.FallbackRoot != ""
}

func renderNaiveGeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = settings.NaivePassword
	}
	return renderer.RenderNaiveCaddyfile(renderer.NaiveConfig{
		Domain:       settings.Domain,
		Email:        settings.Email,
		ListenPort:   inbound.Port,
		Username:     settings.NaiveUsername,
		Password:     password,
		FallbackRoot: settings.FallbackRoot,
	})
}

func renderHysteria2GeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = settings.Hysteria2Password
	}
	return renderer.RenderHysteria2(renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        settings.Domain,
		Password:      password,
		MasqueradeURL: settings.MasqueradeURL,
	})
}
