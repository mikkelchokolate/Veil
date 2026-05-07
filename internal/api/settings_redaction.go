package api

type SettingsRedaction struct{}

func NewSettingsRedaction() SettingsRedaction { return SettingsRedaction{} }

func (SettingsRedaction) Redact(settings Settings) Settings {
	redacted := settings
	if redacted.NaivePassword != "" {
		redacted.NaivePassword = "[REDACTED]"
	}
	if redacted.Hysteria2Password != "" {
		redacted.Hysteria2Password = "[REDACTED]"
	}
	return redacted
}

func redactedSettings(settings Settings) Settings {
	return NewSettingsRedaction().Redact(settings)
}
