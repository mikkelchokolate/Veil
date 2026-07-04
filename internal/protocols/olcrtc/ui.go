package olcrtc

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// InboundFieldSchema returns the dynamic fields for an olcRTC inbound form.
func (Plugin) InboundFieldSchema() []schema.FieldSchema {
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
		{Key: "olcrtcRoomID", Label: "olcRTC Room ID", Type: schema.FieldText, GenerateAction: "room", Scope: "inbound"},
	}
}

// SettingsFieldSchema returns the dynamic fields for global settings.
func (Plugin) SettingsFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "olcrtcAuth", Label: "olcRTC Auth Provider", Type: schema.FieldText, Scope: "settings"},
		{Key: "olcrtcTransport", Label: "olcRTC Transport", Type: schema.FieldText, Scope: "settings"},
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
	if inbound.OlcrtcAuth == "" {
		if v, ok := inbound.ProtocolFields["olcrtcAuth"].(string); ok && v != "" {
			inbound.OlcrtcAuth = v
		} else {
			inbound.OlcrtcAuth = "jitsi"
		}
	}
	if inbound.OlcrtcTransport == "" {
		if v, ok := inbound.ProtocolFields["olcrtcTransport"].(string); ok && v != "" {
			inbound.OlcrtcTransport = v
		} else {
			inbound.OlcrtcTransport = "datachannel"
		}
	}
	if inbound.ProtocolFields["olcrtcAuth"] == nil || inbound.ProtocolFields["olcrtcAuth"] == "" {
		inbound.ProtocolFields["olcrtcAuth"] = inbound.OlcrtcAuth
	}
	if inbound.ProtocolFields["olcrtcTransport"] == nil || inbound.ProtocolFields["olcrtcTransport"] == "" {
		inbound.ProtocolFields["olcrtcTransport"] = inbound.OlcrtcTransport
	}
	if inbound.OlcrtcRoomID == "" && ProviderSupportsAutoRoom(inbound.OlcrtcAuth) {
		room, err := GenerateRoom(inbound.OlcrtcAuth)
		if err != nil {
			return inbound, err
		}
		inbound.OlcrtcRoomID = room
	}
	if inbound.ProtocolFields["olcrtcRoomID"] == nil || inbound.ProtocolFields["olcrtcRoomID"] == "" {
		inbound.ProtocolFields["olcrtcRoomID"] = inbound.OlcrtcRoomID
	}
	if inbound.Password == "" && !isOlcrtcKey(inbound.Password) {
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
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
