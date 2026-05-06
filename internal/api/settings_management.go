package api

import (
	"errors"
	"net"
	"path/filepath"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

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

func normalizeAndValidateSettings(settings *Settings, current Settings) error {
	if settings.PanelListen == "" || settings.Stack == "" || settings.Mode == "" {
		return errors.New("panelListen, stack, and mode are required")
	}
	if settings.Stack != "naive" && settings.Stack != "hysteria2" && settings.Stack != "both" {
		return errors.New("stack must be naive, hysteria2, or both")
	}
	if settings.Domain != "" {
		if err := installer.ValidateDomain(settings.Domain); err != nil {
			return errors.New("domain: " + err.Error())
		}
	}
	if settings.Email != "" {
		if err := installer.ValidateEmail(settings.Email); err != nil {
			return errors.New("email: " + err.Error())
		}
	}
	if settings.PanelListen != "" {
		host, _, err := net.SplitHostPort(settings.PanelListen)
		if err != nil || host == "" {
			return errors.New("panelListen must be host:port")
		}
	}
	if settings.NaivePassword == "[REDACTED]" {
		settings.NaivePassword = current.NaivePassword
	}
	if settings.Hysteria2Password == "[REDACTED]" {
		settings.Hysteria2Password = current.Hysteria2Password
	}
	if settings.FallbackRoot != "" {
		settings.FallbackRoot = filepath.Clean(settings.FallbackRoot)
		if !strings.HasPrefix(filepath.ToSlash(settings.FallbackRoot), "/var/lib/veil") {
			settings.FallbackRoot = filepath.Clean("/var/lib/veil/" + settings.FallbackRoot)
		}
		if !strings.HasPrefix(filepath.ToSlash(settings.FallbackRoot), "/var/lib/veil") {
			return errors.New("fallbackRoot must be within /var/lib/veil")
		}
	}
	return nil
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
