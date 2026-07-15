package settings

import (
	"reflect"

	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

type SettingsRedaction struct {
	fieldSchemas []schema.FieldSchema
}

func NewSettingsRedaction() SettingsRedaction { return NewSettingsRedactionWithFieldSchemas(nil) }

func NewSettingsRedactionWithFieldSchemas(schemas []schema.FieldSchema) SettingsRedaction {
	return SettingsRedaction{fieldSchemas: schemas}
}

func (r SettingsRedaction) Redact(settings Settings) Settings {
	redacted := settings
	if settings.ProtocolFields != nil {
		redacted.ProtocolFields = make(map[string]any, len(settings.ProtocolFields))
		for k, v := range settings.ProtocolFields {
			redacted.ProtocolFields[k] = v
		}
	}
	disclosure := NewCredentialDisclosure()
	for _, f := range r.fieldSchemas {
		if f.Scope != "" && f.Scope != "settings" {
			continue
		}
		if f.Type != schema.FieldPassword {
			continue
		}
		if v, ok := redacted.ProtocolFields[f.Key]; ok {
			if s, ok := v.(string); ok {
				redacted.ProtocolFields[f.Key] = disclosure.Redact(s)
			}
		}
		field := reflect.ValueOf(&redacted).Elem().FieldByName(StructFieldName(f.Key))
		if field.IsValid() && field.Kind() == reflect.String {
			field.SetString(disclosure.Redact(field.String()))
		}
	}
	return redacted
}
