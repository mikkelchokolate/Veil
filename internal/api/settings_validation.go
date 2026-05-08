package api

import (
	"errors"
	"net"
	"path/filepath"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

type SettingsValidation struct{}

func NewSettingsValidation() SettingsValidation { return SettingsValidation{} }

func (SettingsValidation) NormalizeAndValidate(settings *Settings, current Settings) error {
	if settings.PanelListen == "" || settings.Mode == "" {
		return errors.New("panelListen and mode are required")
	}
	stack, ok := NormalizeSettingsStack(settings.Stack)
	if !ok {
		return errors.New("stack must be panel; protocols are configured as Panel inbounds")
	}
	settings.Stack = stack
	if settings.PanelAccess == "" {
		settings.PanelAccess = current.PanelAccess
	}
	if settings.PanelAccess != "" {
		switch settings.PanelAccess {
		case "direct", "local", "caddy":
		default:
			return errors.New("panel access must be direct, local, or caddy")
		}
	}
	if settings.WebBasePath == "" {
		settings.WebBasePath = current.WebBasePath
	} else {
		settings.WebBasePath = normalizeSettingsWebBasePath(settings.WebBasePath)
	}
	if settings.PanelAccess == "caddy" && settings.WebBasePath == "" {
		return errors.New("webBasePath is required for caddy Panel access")
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
	disclosure := NewCredentialDisclosure()
	settings.NaivePassword = disclosure.PreserveRedacted(settings.NaivePassword, current.NaivePassword)
	settings.Hysteria2Password = disclosure.PreserveRedacted(settings.Hysteria2Password, current.Hysteria2Password)
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

func normalizeSettingsWebBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	return "/" + strings.Trim(path, "/") + "/"
}

func normalizeAndValidateSettings(settings *Settings, current Settings) error {
	return NewSettingsValidation().NormalizeAndValidate(settings, current)
}
