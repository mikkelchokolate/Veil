package mieru

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// InboundFieldSchema exposes the aggregate password fallback used when no
// normalized client credentials are attached to the inbound.
func (Plugin) InboundFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "password", Label: "Password", Type: schema.FieldPassword, GenerateAction: "password", Scope: "inbound"},
	}
}

// SettingsFieldSchema returns no dynamic fields for Mieru global settings.
func (Plugin) SettingsFieldSchema() []schema.FieldSchema { return nil }

// Autofill is a no-op for Mieru.
func (Plugin) Autofill(inbound model.Inbound) (model.Inbound, error) {
	// The Panel submits plugin-owned values in ProtocolFields. Promote the
	// generic credential into the canonical runtime field before the inbound
	// catalog applies its fallback password generator. A non-empty dynamic
	// value wins over the flat field: on update the panel echoes the flat
	// password as "[REDACTED]" (restored to the stored value by the API layer),
	// and the field the user actually edited lives in ProtocolFields.
	if inbound.ProtocolFields != nil {
		if password, ok := inbound.ProtocolFields["password"].(string); ok && password != "" && password != veilsettings.RedactedSecret {
			inbound.Password = password
		}
	}
	return inbound, nil
}
