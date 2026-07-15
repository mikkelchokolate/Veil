package settings

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

type SettingsValidation struct {
	fieldSchemas []schema.FieldSchema
}

func NewSettingsValidation() SettingsValidation { return NewSettingsValidationWithFieldSchemas(nil) }

func NewSettingsValidationWithFieldSchemas(schemas []schema.FieldSchema) SettingsValidation {
	return SettingsValidation{fieldSchemas: schemas}
}

func (v SettingsValidation) NormalizeAndValidate(settings *Settings, current Settings) error {
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
	webBasePath := settings.WebBasePath
	if webBasePath == "" {
		webBasePath = current.WebBasePath
	}
	if webBasePath != "" {
		normalized, err := webbasepath.NormalizeOptional(webBasePath)
		if err != nil {
			return fmt.Errorf("webBasePath: %w", err)
		}
		settings.WebBasePath = normalized
	} else {
		settings.WebBasePath = ""
	}
	if settings.PanelAccess == "caddy" && settings.WebBasePath == "" {
		return errors.New("webBasePath is required for caddy Panel access")
	}
	if err := v.normalizeProtocolFields(settings, current); err != nil {
		return err
	}

	if settings.PanelDomain == "" {
		settings.PanelDomain = current.PanelDomain
	}
	if settings.PanelEmail == "" {
		settings.PanelEmail = current.PanelEmail
	}
	if settings.DefaultAcmeEmail == "" {
		settings.DefaultAcmeEmail = current.DefaultAcmeEmail
	}
	if settings.PanelPublicPort == 0 {
		settings.PanelPublicPort = current.PanelPublicPort
	}
	if settings.PanelPublicPort == 0 {
		settings.PanelPublicPort = 443
	}
	if settings.DefaultInboundPublicPort == 0 {
		settings.DefaultInboundPublicPort = current.DefaultInboundPublicPort
	}
	if settings.DefaultInboundPublicPort == 0 {
		settings.DefaultInboundPublicPort = 443
	}
	if settings.AcmeChallengeMode == "" {
		settings.AcmeChallengeMode = current.AcmeChallengeMode
	}
	if settings.AcmeChallengeMode == "" {
		settings.AcmeChallengeMode = "tls-alpn-01"
	}

	if settings.PanelAccess == "caddy" {
		if strings.TrimSpace(settings.PanelDomain) == "" {
			settings.PanelDomain = strings.TrimSpace(settings.Domain)
		}
		if strings.TrimSpace(settings.PanelEmail) == "" {
			settings.PanelEmail = strings.TrimSpace(settings.Email)
		}
		if strings.TrimSpace(settings.PanelDomain) == "" || strings.TrimSpace(settings.PanelEmail) == "" {
			return errors.New("panelDomain and panelEmail are required for caddy Panel access")
		}
	}
	if settings.PanelPublicPort < 1 || settings.PanelPublicPort > 65535 {
		return errors.New("panelPublicPort must be between 1 and 65535")
	}
	if settings.DefaultInboundPublicPort < 1 || settings.DefaultInboundPublicPort > 65535 {
		return errors.New("defaultInboundPublicPort must be between 1 and 65535")
	}
	switch settings.AcmeChallengeMode {
	case "http-01", "tls-alpn-01":
	case "dns-01":
		return errors.New("dns-01 ACME challenge requires DNS provider credentials, which are not yet configured")
	default:
		return errors.New("acmeChallengeMode must be http-01, tls-alpn-01, or dns-01")
	}
	if settings.PanelDomain != "" {
		if err := hostenv.ValidateDomain(settings.PanelDomain); err != nil {
			return errors.New("panelDomain: " + err.Error())
		}
	}
	if settings.PanelEmail != "" {
		if err := hostenv.ValidateEmail(settings.PanelEmail); err != nil {
			return errors.New("panelEmail: " + err.Error())
		}
	}
	if settings.DefaultAcmeEmail != "" {
		if err := hostenv.ValidateEmail(settings.DefaultAcmeEmail); err != nil {
			return errors.New("defaultAcmeEmail: " + err.Error())
		}
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
	if settings.FallbackRoot != "" {
		if err := normalizeFallbackRoot(&settings.FallbackRoot); err != nil {
			return err
		}
		settings.ProtocolFields["fallbackRoot"] = settings.FallbackRoot
	}
	if root, ok := settings.ProtocolFields["fallbackRoot"].(string); ok && root != "" {
		if err := normalizeFallbackRoot(&root); err != nil {
			return err
		}
		settings.ProtocolFields["fallbackRoot"] = root
		settings.FallbackRoot = root
	}
	return nil
}

func (v SettingsValidation) normalizeProtocolFields(settings *Settings, current Settings) error {
	if settings.ProtocolFields == nil {
		settings.ProtocolFields = map[string]any{}
	}
	for _, f := range v.fieldSchemas {
		if f.Scope != "" && f.Scope != "settings" {
			continue
		}
		val, provided := protocolFieldUpdateValue(settings, f)
		if !provided {
			if cv, ok := current.ProtocolFields[f.Key]; ok {
				val = cv
			} else if cv, ok := flatFieldValue(current, f.Key); ok {
				val = cv
			} else if f.Default != nil {
				val = f.Default
			}
		}
		if f.Type == schema.FieldPassword {
			if s, ok := val.(string); ok && s == RedactedSecret {
				if cv, ok := current.ProtocolFields[f.Key].(string); ok && cv != "" && cv != RedactedSecret {
					val = cv
				} else if cv, ok := flatFieldValue(current, f.Key); ok {
					if s2, ok := cv.(string); ok && s2 != "" && s2 != RedactedSecret {
						val = s2
					} else {
						val = ""
					}
				} else {
					val = ""
				}
			}
		}
		if val != nil {
			settings.ProtocolFields[f.Key] = val
			setFlatFieldValue(settings, f.Key, val)
		}
	}
	return nil
}

func protocolFieldUpdateValue(settings *Settings, f schema.FieldSchema) (any, bool) {
	if v, ok := settings.ProtocolFields[f.Key]; ok {
		return v, true
	}
	return flatFieldValue(*settings, f.Key)
}

func flatFieldValue(settings Settings, key string) (any, bool) {
	v := reflect.ValueOf(settings)
	field := v.FieldByName(StructFieldName(key))
	if !field.IsValid() {
		return nil, false
	}
	switch field.Kind() {
	case reflect.String:
		if s := field.String(); s != "" {
			return s, true
		}
	case reflect.Bool:
		return field.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(field.Int()), true
	}
	return nil, false
}

func setFlatFieldValue(settings *Settings, key string, val any) {
	field := reflect.ValueOf(settings).Elem().FieldByName(StructFieldName(key))
	if !field.IsValid() {
		return
	}
	switch field.Kind() {
	case reflect.String:
		if s, ok := val.(string); ok {
			field.SetString(s)
		}
	case reflect.Bool:
		if b, ok := val.(bool); ok {
			field.SetBool(b)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if i, ok := val.(int); ok {
			field.SetInt(int64(i))
		}
	}
}

func StructFieldName(key string) string {
	if key == "" {
		return ""
	}
	return strings.ToUpper(key[:1]) + key[1:]
}

func normalizeFallbackRoot(root *string) error {
	const base = "/var/lib/veil"

	raw := strings.TrimSpace(*root)
	if raw == "" {
		*root = ""
		return nil
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(raw), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return errors.New("fallbackRoot must not contain parent-directory traversal")
		}
	}

	candidate := raw
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate = filepath.Clean(candidate)
	relative, err := filepath.Rel(base, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("fallbackRoot must be within /var/lib/veil")
	}
	*root = filepath.ToSlash(candidate)
	return nil
}

// NormalizeWebBasePath is retained for generated-config callers that cannot
// return a validation error. Invalid input fails closed to the empty/root
// representation instead of being returned raw into a Caddyfile template.
func NormalizeWebBasePath(path string) string {
	normalized, err := webbasepath.NormalizeOptional(path)
	if err != nil {
		return ""
	}
	return normalized
}
