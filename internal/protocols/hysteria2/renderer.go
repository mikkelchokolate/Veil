package hysteria2

import (
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// RenderConfig generates one Hysteria2 config per enabled inbound.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}
	var artifacts []generatedconfig.GeneratedConfigArtifact
	for _, inbound := range input.Inbounds {
		body, err := renderHysteria2(input.Settings, inbound, input.Warp)
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
		ValidationCommand: func(path string) []string {
			return []string{"hysteria", "server", "--config", path, "--check"}
		},
	}
}

func renderHysteria2(settings model.Settings, inbound model.Inbound, warp model.WarpConfig) (string, error) {
	password := hysteria2Password(settings, inbound)
	access, err := clientaccess.BuildClientAccess(settings, inbound)
	if err != nil {
		return "", err
	}
	url := masqueradeURL(settings, inbound)
	hystConfig := renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        settings.Domain,
		Password:      password,
		Users:         access.Hysteria2Users(),
		MasqueradeURL: url,
	}
	if settings.PanelAccess == "caddy" && settings.Domain != "" {
		hystConfig.CertPath = "/etc/veil/certs/" + settings.Domain + ".crt"
		hystConfig.KeyPath = "/etc/veil/certs/" + settings.Domain + ".key"
	}
	if warp.Enabled {
		socksPort := warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		hystConfig.Upstream = "127.0.0.1:" + strconv.Itoa(socksPort)
	}
	return renderer.RenderHysteria2(hystConfig)
}
