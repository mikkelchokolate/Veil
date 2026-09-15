package olcrtc

import (
	"errors"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// RenderConfig generates one olcRTC config per enabled inbound.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}
	var artifacts []generatedconfig.GeneratedConfigArtifact
	for _, inbound := range input.Inbounds {
		body, err := renderOlcrtc(input.Settings, inbound, input.Warp)
		if err != nil {
			return nil, false, err
		}
		subpath := "olcrtc/" + inbound.Name + ".yaml"
		artifacts = append(artifacts, generatedconfig.GeneratedConfigArtifact{
			Path: input.Paths.Generated(subpath),
			Body: body,
		})
	}
	return artifacts, true, nil
}

// ArtifactSpec returns the artifact metadata for olcRTC configs.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.OlcrtcConfigSubpath,
		ValidationName: "olcrtc",
	}
}

func renderOlcrtc(settings model.Settings, inbound model.Inbound, warp model.WarpConfig) (string, error) {
	password := olcrtcKey(inbound)
	if password == "" {
		// Rendering must be deterministic. Credential generation belongs to the
		// persisted create/autofill/room workflow; synthesizing a key here makes
		// the server config diverge from client export on the very next render.
		return "", errors.New("olcrtc encryption key is required before rendering")
	}
	cfg := renderer.OlcrtcConfig{
		Auth:      olcrtcAuth(settings, inbound),
		RoomID:    olcrtcRoomID(settings, inbound),
		Key:       password,
		Transport: olcrtcTransport(settings, inbound),
		DNS:       "",
	}
	if warp.Enabled {
		port := warp.SocksPort
		if port == 0 {
			port = 40000
		}
		cfg.SocksAddr = "127.0.0.1"
		cfg.SocksPort = port
	}
	return renderer.RenderOlcrtc(cfg)
}
