package hysteria2

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// InboundFieldSchema returns the dynamic fields for a Hysteria2 inbound form.
func (Plugin) InboundFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: model.InboundDomainField, Label: "Domain", Type: schema.FieldText, Required: true, Placeholder: "Public domain used for TLS/SNI and client export.", Scope: "inbound"},
		{Key: model.InboundEmailField, Label: "ACME email", Type: schema.FieldText, Placeholder: "Required when this inbound overrides the global domain.", Scope: "inbound"},
		{Key: "hysteria2Password", Label: "Hysteria2 Password", Type: schema.FieldPassword, GenerateAction: "password", Scope: "inbound"},
		{Key: "masqueradeURL", Label: "Masquerade URL", Type: schema.FieldText, Default: "https://example.com", Scope: "inbound"},
		{Key: "hysteria2Insecure", Label: "Insecure mode (allow self-signed server certificate)", Type: schema.FieldCheckbox, Scope: "inbound"},
	}
}

// SettingsFieldSchema returns the dynamic fields for global settings.
func (Plugin) SettingsFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "hysteria2Password", Label: "Hysteria2 Password", Type: schema.FieldPassword, Scope: "settings"},
		{Key: "masqueradeURL", Label: "Masquerade URL", Type: schema.FieldText, Scope: "settings"},
		{Key: "hysteria2Insecure", Label: "Insecure mode (allow self-signed server certificate)", Type: schema.FieldCheckbox, Scope: "settings"},
	}
}

// Autofill promotes the panel-submitted ProtocolFields password into the
// canonical flat Password field the renderer consumes, and applies defaults.
func (Plugin) Autofill(inbound model.Inbound) (model.Inbound, error) {
	if inbound.Password == "" && inbound.ProtocolFields != nil {
		if password, ok := inbound.ProtocolFields["hysteria2Password"].(string); ok {
			inbound.Password = password
		}
	}
	return inbound, nil
}
