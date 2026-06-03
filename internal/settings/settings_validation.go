package settings

import (
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

type SettingsValidation struct{}

func NewSettingsValidation() SettingsValidation { return SettingsValidation{} }

func (SettingsValidation) NormalizeAndValidate(settings *Settings, current Settings) error {
	if settings.PanelListen == "" || settings.Mode == "" {
		return errors.New("panelListen and mode are required")
	}
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
		settings.WebBasePath = NormalizeWebBasePath(settings.WebBasePath)
	}
	if settings.PanelAccess == "caddy" && settings.WebBasePath == "" {
		return errors.New("webBasePath is required for caddy Panel access")
	}
	if settings.OlcrtcAuth == "" {
		settings.OlcrtcAuth = current.OlcrtcAuth
	}
	if settings.OlcrtcTransport == "" {
		settings.OlcrtcTransport = current.OlcrtcTransport
	}
	if settings.OlcrtcRoomID == "" {
		settings.OlcrtcRoomID = current.OlcrtcRoomID
	}
	if settings.PanelAccess == "caddy" && (strings.TrimSpace(settings.Domain) == "" || strings.TrimSpace(settings.Email) == "") {
		return errors.New("--domain and --email are required for caddy Panel access")
	}
	if settings.Domain != "" {
		if err := hostenv.ValidateDomain(settings.Domain); err != nil {
			return errors.New("domain: " + err.Error())
		}
	}
	if settings.Email != "" {
		if err := hostenv.ValidateEmail(settings.Email); err != nil {
			return errors.New("email: " + err.Error())
		}
	}
	if settings.PanelListen != "" {
		host, portStr, err := net.SplitHostPort(settings.PanelListen)
		if err != nil || host == "" {
			return errors.New("panelListen must be host:port")
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return errors.New("panelListen port must be a valid integer between 1 and 65535")
		}
	}
	disclosure := NewCredentialDisclosure()
	settings.NaivePassword = disclosure.PreserveRedacted(settings.NaivePassword, current.NaivePassword)
	settings.Hysteria2Password = disclosure.PreserveRedacted(settings.Hysteria2Password, current.Hysteria2Password)
	settings.OlcrtcAuth = disclosure.PreserveRedacted(settings.OlcrtcAuth, current.OlcrtcAuth)
	if settings.FallbackRoot != "" {
		settings.FallbackRoot = filepath.Clean(settings.FallbackRoot)
		if !strings.HasPrefix(filepath.ToSlash(settings.FallbackRoot), "/var/lib/veil") {
			settings.FallbackRoot = filepath.Clean("/var/lib/veil/" + settings.FallbackRoot)
		}
		if !strings.HasPrefix(filepath.ToSlash(settings.FallbackRoot), "/var/lib/veil") {
			return errors.New("fallbackRoot must be within /var/lib/veil")
		}
		settings.FallbackRoot = filepath.ToSlash(settings.FallbackRoot)
	}
	return nil
}

func NormalizeWebBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	return "/" + strings.Trim(path, "/") + "/"
}
