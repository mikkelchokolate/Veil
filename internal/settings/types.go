package settings

import "github.com/veil-panel/veil/internal/model"

type Settings = model.Settings
type Validation = SettingsValidation
type Redaction = SettingsRedaction

func NewValidation() Validation { return NewSettingsValidation() }
func NewRedaction() Redaction   { return NewSettingsRedaction() }
