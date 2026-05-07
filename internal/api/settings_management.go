package api

import "errors"

var ErrSettingsInvalid = errors.New("settings invalid")

type SettingsManagement struct {
	settings *Settings
	save     func() error
}

func NewSettingsManagement(settings *Settings, save func() error) SettingsManagement {
	if save == nil {
		save = func() error { return nil }
	}
	return SettingsManagement{settings: settings, save: save}
}

func (m SettingsManagement) Get() Settings {
	if m.settings == nil {
		return Settings{}
	}
	return redactedSettings(*m.settings)
}

func (m SettingsManagement) Update(update Settings) (Settings, error) {
	current := Settings{}
	if m.settings != nil {
		current = *m.settings
	}
	if err := normalizeAndValidateSettings(&update, current); err != nil {
		return Settings{}, err
	}
	if m.settings != nil {
		*m.settings = update
	}
	if err := m.save(); err != nil {
		return Settings{}, err
	}
	return redactedSettings(update), nil
}

func redactedSettings(settings Settings) Settings {
	redacted := settings
	if redacted.NaivePassword != "" {
		redacted.NaivePassword = "[REDACTED]"
	}
	if redacted.Hysteria2Password != "" {
		redacted.Hysteria2Password = "[REDACTED]"
	}
	return redacted
}
