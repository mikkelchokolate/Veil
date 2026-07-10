package settings

import (
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

type Settings = model.Settings
type Validation = SettingsValidation
type Redaction = SettingsRedaction

func NewValidation() Validation { return NewSettingsValidation() }
func NewValidationWithFieldSchemas(schemas []schema.FieldSchema) Validation {
	return NewSettingsValidationWithFieldSchemas(schemas)
}
func NewRedaction() Redaction { return NewSettingsRedaction() }
func NewRedactionWithFieldSchemas(schemas []schema.FieldSchema) Redaction {
	return NewSettingsRedactionWithFieldSchemas(schemas)
}
