package mieru

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// InboundFieldSchema returns no dynamic fields for Mieru (uses password + profiles).
func (Plugin) InboundFieldSchema() []schema.FieldSchema { return nil }

// SettingsFieldSchema returns no dynamic fields for Mieru global settings.
func (Plugin) SettingsFieldSchema() []schema.FieldSchema { return nil }

// Autofill is a no-op for Mieru.
func (Plugin) Autofill(inbound model.Inbound) (model.Inbound, error) {
	return inbound, nil
}
