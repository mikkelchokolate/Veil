package olcrtc

import (
	"encoding/hex"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// InboundFieldSchema returns the dynamic fields for an olcRTC inbound form.
func (Plugin) InboundFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "password", Label: "Encryption key", Type: schema.FieldPassword, GenerateAction: "hex64", Scope: "inbound"},
		{
			Key:     "olcrtcAuth",
			Label:   "olcRTC Auth Provider",
			Type:    schema.FieldSelect,
			Default: "jitsi",
			Options: []schema.FieldOption{
				{Label: "jitsi", Value: "jitsi", Attributes: map[string]string{"data-autoroom": "true"}},
				{Label: "telemost", Value: "telemost", Attributes: map[string]string{"data-autoroom": "false"}},
				{Label: "wbstream", Value: "wbstream", Attributes: map[string]string{"data-autoroom": "false"}},
			},
			Scope: "inbound",
		},
		{
			Key:     "olcrtcTransport",
			Label:   "olcRTC Transport",
			Type:    schema.FieldSelect,
			Default: "datachannel",
			Options: []schema.FieldOption{
				{Label: "datachannel", Value: "datachannel"},
				{Label: "vp8channel", Value: "vp8channel"},
				{Label: "seichannel", Value: "seichannel"},
				{Label: "videochannel", Value: "videochannel"},
			},
			Scope: "inbound",
		},
		{Key: "olcrtcRoomID", Label: "olcRTC Room ID", Type: schema.FieldText, GenerateAction: "room", GenerateActionField: "olcrtcAuth", Scope: "inbound"},
	}
}

// SettingsFieldSchema returns the dynamic fields for global settings.
func (Plugin) SettingsFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{
			Key:     "olcrtcAuth",
			Label:   "olcRTC Auth Provider",
			Type:    schema.FieldSelect,
			Default: "jitsi",
			Options: []schema.FieldOption{
				{Label: "jitsi", Value: "jitsi", Attributes: map[string]string{"data-autoroom": "true"}},
				{Label: "telemost", Value: "telemost", Attributes: map[string]string{"data-autoroom": "false"}},
				{Label: "wbstream", Value: "wbstream", Attributes: map[string]string{"data-autoroom": "false"}},
			},
			Scope: "settings",
		},
		{
			Key:     "olcrtcTransport",
			Label:   "olcRTC Transport",
			Type:    schema.FieldSelect,
			Default: "datachannel",
			Options: []schema.FieldOption{
				{Label: "datachannel", Value: "datachannel"},
				{Label: "vp8channel", Value: "vp8channel"},
				{Label: "seichannel", Value: "seichannel"},
				{Label: "videochannel", Value: "videochannel"},
			},
			Scope: "settings",
		},
		{Key: "olcrtcRoomID", Label: "olcRTC Room ID", Type: schema.FieldText, Scope: "settings"},
	}
}

// Autofill generates a working olcRTC config for one-click provisioning.
// It writes both the dynamic ProtocolFields map and the legacy flat fields so
// existing consumers and tests keep working during the transition.
func (Plugin) Autofill(inbound model.Inbound) (model.Inbound, error) {
	if inbound.ProtocolFields == nil {
		inbound.ProtocolFields = map[string]any{}
	}
	// Dynamic Panel fields are submitted through ProtocolFields. A non-empty
	// value wins over the flat field: on update the panel echoes the flat
	// password as "[REDACTED]" (restored to the stored value by the API layer),
	// and the key the user actually edited lives in ProtocolFields. Preserve an
	// explicitly generated/provided encryption key instead of replacing it.
	if inbound.ProtocolFields != nil {
		if password, ok := inbound.ProtocolFields["password"].(string); ok && password != "" && password != veilsettings.RedactedSecret {
			inbound.Password = password
		}
	}
	// Auth and transport resolve protocolFields-first: the SPA edits dynamic
	// fields while echoing the stale flat value, so a flat-first read here
	// would generate a room for the OLD provider (audit #133 F1).
	if v, ok := inbound.ProtocolFields["olcrtcAuth"].(string); ok && v != "" {
		inbound.OlcrtcAuth = v
	} else if inbound.OlcrtcAuth == "" {
		inbound.OlcrtcAuth = "jitsi"
	}
	if v, ok := inbound.ProtocolFields["olcrtcTransport"].(string); ok && v != "" {
		inbound.OlcrtcTransport = v
	} else if inbound.OlcrtcTransport == "" {
		inbound.OlcrtcTransport = "datachannel"
	}
	if inbound.ProtocolFields["olcrtcAuth"] == nil || inbound.ProtocolFields["olcrtcAuth"] == "" {
		inbound.ProtocolFields["olcrtcAuth"] = inbound.OlcrtcAuth
	}
	if inbound.ProtocolFields["olcrtcTransport"] == nil || inbound.ProtocolFields["olcrtcTransport"] == "" {
		inbound.ProtocolFields["olcrtcTransport"] = inbound.OlcrtcTransport
	}
	// Auto-generate a room only when the user never touched the field (no
	// protocolFields key at all). An explicit "" in protocolFields means the
	// operator cleared it, and regenerating would silently undo the clear
	// (audit #133/#139).
	if _, touched := inbound.ProtocolFields["olcrtcRoomID"]; !touched && inbound.OlcrtcRoomID == "" && ProviderSupportsAutoRoom(inbound.OlcrtcAuth) {
		room, err := GenerateRoom(inbound.OlcrtcAuth)
		if err != nil {
			return inbound, err
		}
		inbound.OlcrtcRoomID = room
	}
	// Room resolves protocolFields-first like auth/transport: the SPA edits
	// the dynamic field while echoing a stale flat room.
	if v, ok := inbound.ProtocolFields["olcrtcRoomID"].(string); ok && v != "" {
		inbound.OlcrtcRoomID = v
	}
	if inbound.ProtocolFields["olcrtcRoomID"] == nil || inbound.ProtocolFields["olcrtcRoomID"] == "" {
		inbound.ProtocolFields["olcrtcRoomID"] = inbound.OlcrtcRoomID
	}
	if inbound.Password == "" || !isOlcrtcKey(inbound.Password) {
		key, err := generateRandomHex(64)
		if err != nil {
			return inbound, err
		}
		inbound.Password = key
	}
	return inbound, nil
}

func isOlcrtcKey(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n/2)
	if _, err := randRead(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
