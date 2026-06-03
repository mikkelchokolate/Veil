package settings

type SettingsRedaction struct{}

func NewSettingsRedaction() SettingsRedaction { return SettingsRedaction{} }

func (SettingsRedaction) Redact(settings Settings) Settings {
	redacted := settings
	disclosure := NewCredentialDisclosure()
	redacted.NaivePassword = disclosure.Redact(redacted.NaivePassword)
	redacted.Hysteria2Password = disclosure.Redact(redacted.Hysteria2Password)
	redacted.OlcrtcAuth = disclosure.Redact(redacted.OlcrtcAuth)
	return redacted
}
