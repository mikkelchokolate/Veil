package olcrtc

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Plugin implements the olcRTC protocol.
type Plugin struct{}

// New creates an olcRTC plugin instance.
func New() *Plugin { return &Plugin{} }

func (Plugin) Protocol() string        { return "olcrtc" }
func (Plugin) DisplayName() string     { return "olcRTC" }
func (Plugin) Transports() []string    { return []string{"udp"} }
func (Plugin) RequiresCaddy() bool     { return false }
func (Plugin) FirewallService() string { return "" }
func (Plugin) MaxEnabled() int         { return 0 }

// GenerateRoom implements the RoomGenerator capability for olcRTC.
func (Plugin) GenerateRoom(provider string) (string, error) {
	return GenerateRoom(provider)
}

func protocolString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(s)
}

// olcrtcKey resolves the effective encryption key using the same precedence
// as the dynamic inbound form: protocolFields wins over the legacy flat field.
// Validation, server rendering and client export must agree on this value.
func olcrtcKey(inbound model.Inbound) string {
	return strings.TrimSpace(protocolString(inbound.ProtocolFields, "password", inbound.Password))
}

func olcrtcAuth(settings model.Settings, inbound model.Inbound) string {
	auth := protocolString(inbound.ProtocolFields, "olcrtcAuth", "")
	if auth == "" {
		auth = inbound.OlcrtcAuth
	}
	if auth == "" {
		auth = protocolString(settings.ProtocolFields, "olcrtcAuth", "")
	}
	if auth == "" {
		auth = settings.OlcrtcAuth
	}
	if auth == "" {
		auth = "jitsi"
	}
	return auth
}

func olcrtcTransport(settings model.Settings, inbound model.Inbound) string {
	transport := protocolString(inbound.ProtocolFields, "olcrtcTransport", "")
	if transport == "" {
		transport = inbound.OlcrtcTransport
	}
	if transport == "" {
		transport = protocolString(settings.ProtocolFields, "olcrtcTransport", "")
	}
	if transport == "" {
		transport = settings.OlcrtcTransport
	}
	if transport == "" {
		transport = "datachannel"
	}
	return transport
}

func olcrtcRoomID(settings model.Settings, inbound model.Inbound) string {
	room := protocolString(inbound.ProtocolFields, "olcrtcRoomID", "")
	if room == "" {
		room = inbound.OlcrtcRoomID
	}
	if room == "" {
		room = protocolString(settings.ProtocolFields, "olcrtcRoomID", "")
	}
	if room == "" {
		room = settings.OlcrtcRoomID
	}
	return room
}
