package settings

import "github.com/mikkelchokolate/Veil/internal/protocols/schema"

func testSettingsFieldSchemas() []schema.FieldSchema {
	return []schema.FieldSchema{
		// naiveproxy
		{Key: "naiveUsername", Label: "Naive Username", Type: schema.FieldText, Default: "veil", Scope: "settings"},
		{Key: "naivePassword", Label: "Naive Password", Type: schema.FieldPassword, Scope: "settings"},
		{Key: "fallbackRoot", Label: "Fallback Root", Type: schema.FieldText, Default: "/var/lib/veil/www", Scope: "settings"},
		// hysteria2
		{Key: "hysteria2Password", Label: "Hysteria2 Password", Type: schema.FieldPassword, Scope: "settings"},
		{Key: "masqueradeURL", Label: "Masquerade URL", Type: schema.FieldText, Default: "https://example.com", Scope: "settings"},
		{Key: "hysteria2Insecure", Label: "Insecure mode", Type: schema.FieldCheckbox, Scope: "settings"},
		// olcrtc
		{Key: "olcrtcAuth", Label: "olcRTC Auth Provider", Type: schema.FieldSelect, Default: "jitsi", Scope: "settings"},
		{Key: "olcrtcTransport", Label: "olcRTC Transport", Type: schema.FieldSelect, Default: "datachannel", Scope: "settings"},
		{Key: "olcrtcRoomID", Label: "olcRTC Room ID", Type: schema.FieldText, Scope: "settings"},
	}
}
