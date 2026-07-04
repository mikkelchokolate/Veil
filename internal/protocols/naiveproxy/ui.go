package naiveproxy

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// InboundFieldSchema returns the dynamic fields for an inbound naiveproxy form.
func (Plugin) InboundFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "naiveUsername", Label: "Naive Username", Type: schema.FieldText, Default: "veil", Scope: "inbound"},
		{Key: "naivePassword", Label: "Naive Password", Type: schema.FieldPassword, GenerateAction: "password", Scope: "inbound"},
		{Key: "fallbackRoot", Label: "Fallback Root", Type: schema.FieldText, Default: "/var/lib/veil/www", Scope: "inbound"},
	}
}

// SettingsFieldSchema returns the dynamic fields for global settings.
func (Plugin) SettingsFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "naiveUsername", Label: "Naive Username", Type: schema.FieldText, Default: "veil", Scope: "settings"},
		{Key: "naivePassword", Label: "Naive Password", Type: schema.FieldPassword, Scope: "settings"},
		{Key: "fallbackRoot", Label: "Fallback Root", Type: schema.FieldText, Default: "/var/lib/veil/www", Scope: "settings"},
	}
}

// Autofill is a no-op for naiveproxy.
func (Plugin) Autofill(inbound model.Inbound) (model.Inbound, error) {
	return inbound, nil
}
