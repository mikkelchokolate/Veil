package api

type SettingsRedaction struct{}

func NewSettingsRedaction() SettingsRedaction { return SettingsRedaction{} }

func (SettingsRedaction) Redact(settings Settings) Settings {
	redacted := settings
	disclosure := NewCredentialDisclosure()
	redacted.NaivePassword = disclosure.Redact(redacted.NaivePassword)
	redacted.Hysteria2Password = disclosure.Redact(redacted.Hysteria2Password)
	return redacted
}

func redactedSettings(settings Settings) Settings {
	return NewSettingsRedaction().Redact(settings)
}
