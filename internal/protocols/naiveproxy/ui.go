package naiveproxy

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// InboundFieldSchema returns the dynamic fields for an inbound naiveproxy form.
func (Plugin) InboundFieldSchema() []schema.FieldSchema {
	return []schema.FieldSchema{
		{Key: "domain", Label: "Domain", Type: schema.FieldText, Required: true, Placeholder: "Public domain used for TLS/SNI and client export.", Scope: "inbound"},
		{Key: "email", Label: "ACME email", Type: schema.FieldText, Placeholder: "Optional explicit ACME contact for this domain.", Scope: "inbound"},
		{Key: "publicPort", Label: "Public port", Type: schema.FieldNumber, Default: 443, Placeholder: "Port Caddy listens on for this inbound.", Scope: "inbound"},
		{Key: "transport", Label: "Transport", Type: schema.FieldSelect, Required: true, Default: "tcp", Options: []schema.FieldOption{{Label: "tcp", Value: "tcp"}, {Label: "quic", Value: "quic"}, {Label: "dual", Value: "dual"}}, Placeholder: "tcp=HTTPS/H2, quic=HTTP/3/QUIC, dual=both.", Scope: "inbound"},
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
		{Key: "panelAccess", Label: "Panel Access", Type: schema.FieldSelect, Default: "local", Options: []schema.FieldOption{{Label: "local", Value: "local"}, {Label: "direct", Value: "direct"}, {Label: "caddy", Value: "caddy"}}, Scope: "settings"},
		{Key: "panelDomain", Label: "Panel Domain", Type: schema.FieldText, Scope: "settings", Placeholder: "Public domain used for Panel Caddy TLS/SNI."},
		{Key: "panelEmail", Label: "Panel ACME Email", Type: schema.FieldText, Scope: "settings", Placeholder: "ACME contact email for Panel Caddy certificate."},
		{Key: "panelPublicPort", Label: "Panel Public Port", Type: schema.FieldNumber, Default: 443, Scope: "settings", Placeholder: "Port Caddy listens on for Panel access."},
	}
}

// Autofill is a no-op for naiveproxy.
func (Plugin) Autofill(inbound model.Inbound) (model.Inbound, error) {
	return inbound, nil
}
