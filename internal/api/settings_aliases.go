package api

import veilsettings "github.com/veil-panel/veil/internal/settings"

const RedactedSecret = veilsettings.RedactedSecret

type CredentialDisclosure = veilsettings.CredentialDisclosure
type SettingsValidation = veilsettings.SettingsValidation
type SettingsRedaction = veilsettings.SettingsRedaction

func NewCredentialDisclosure() CredentialDisclosure { return veilsettings.NewCredentialDisclosure() }
func NewSettingsValidation() SettingsValidation     { return veilsettings.NewSettingsValidation() }
func NewSettingsRedaction() SettingsRedaction       { return veilsettings.NewSettingsRedaction() }

func normalizeAndValidateSettings(settings *Settings, current Settings) error {
	return veilsettings.NewSettingsValidation().NormalizeAndValidate(settings, current)
}

func redactedSettings(settings Settings) Settings {
	return veilsettings.NewSettingsRedaction().Redact(settings)
}

func normalizeSettingsWebBasePath(path string) string {
	return veilsettings.NormalizeWebBasePath(path)
}
