package api

import "github.com/mikkelchokolate/Veil/internal/protocols"

func requiresProtocolRenderSettings(p protocols.ProtocolPlugin) bool {
	return protocols.RequiresRenderSettings(p)
}
