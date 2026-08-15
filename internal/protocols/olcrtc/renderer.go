package olcrtc

import (
	"encoding/hex"

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
		body, err := renderOlcrtc(input.Settings, inbound)
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

func renderOlcrtc(settings model.Settings, inbound model.Inbound) (string, error) {
	password := olcrtcKey(inbound)
	if password == "" {
		// Apply validation rejects an empty effective key. Keep this defensive
		// fallback for direct renderer callers, but never ignore a key supplied
		// through protocolFields.
		bytes := make([]byte, 32)
		if _, err := randRead(bytes); err != nil {
			return "", err
		}
		password = hex.EncodeToString(bytes)
	}
	return renderer.RenderOlcrtc(renderer.OlcrtcConfig{
		Auth:      olcrtcAuth(settings, inbound),
		RoomID:    olcrtcRoomID(settings, inbound),
		Key:       password,
		Transport: olcrtcTransport(settings, inbound),
		DNS:       "",
	})
}
