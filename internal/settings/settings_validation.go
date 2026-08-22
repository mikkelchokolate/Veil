package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// Drop keys that no registered settings-scoped schema declares. The
	// redaction sentinel must never be persisted verbatim under an unknown
	// key, and unknown keys are never consumed by renderers.
	schemaKeys := map[string]schema.FieldSchema{}
	for _, f := range v.fieldSchemas {
		if f.Scope == "" || f.Scope == "settings" {
			schemaKeys[f.Key] = f
		}
	}
	for key := range settings.ProtocolFields {
		if _, ok := schemaKeys[key]; !ok {
			// Log legacy keys so a future version can migrate instead of
			// silently erasing data written by older releases. Never log the
			// value: the key may be a password sentinel.
			slog.Debug("settings: dropping unknown protocol field", "field", key)
			delete(settings.ProtocolFields, key)
		}
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
			if val == nil {
				// Distinguish "key absent" (fine: nothing to set) from an
				// explicit JSON null (invalid: would persist nil and drop
				// the secret from rendered configs).
				if _, present := settings.ProtocolFields[f.Key]; present {
					return fmt.Errorf("protocolFields.%s must be a string", f.Key)
				}
				continue
			}
			s, isString := val.(string)
			if !isString {
				return fmt.Errorf("protocolFields.%s must be a string", f.Key)
			}
			if s == RedactedSecret {
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
		if f.Type == schema.FieldSelect && len(f.Options) > 0 && provided {
			// Validate only values the client actually submitted. Values
			// inherited from current or defaults may predate the declared
			// options (live states written by older releases); rejecting
			// them would turn every PUT into a permanent 400 with no
			// migration path.
			s, isString := val.(string)
			if !isString {
				return fmt.Errorf("protocolFields.%s must be a string", f.Key)
			}
			if s == "" {
				continue
			}
			valid := false
			for _, option := range f.Options {
				if option.Value == s {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("protocolFields.%s must be one of %s", f.Key, selectOptionValues(f.Options))
			}
		}
		if val != nil {
			settings.ProtocolFields[f.Key] = val
			setFlatFieldValue(settings, f.Key, val)
		}
	}
	return nil
}

func selectOptionValues(options []schema.FieldOption) string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, strconv.Quote(option.Value))
	}
	return strings.Join(values, ", ")
}

func protocolFieldUpdateValue(settings *Settings, f schema.FieldSchema) (any, bool) {
	// A schema key that is also a dedicated top-level Settings field (e.g.
	// panelAccess) must prefer the explicit top-level value. Echoing back a
	// redacted GET response leaves a stale ProtocolFields[key] that would
	// otherwise silently override the top-level change the user requested.
	if v, ok := flatFieldValue(*settings, f.Key); ok {
		return v, true
	}
	if v, ok := settings.ProtocolFields[f.Key]; ok {
		return v, true
	}
	return nil, false
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
		// A zero flat bool means "not provided" (the client did not send the
		// flat copy, e.g. the legacy panel which only submits protocolFields).
		// Treating false as absent lets the protocolFields copy win; a client
		// that explicitly sends false with an empty protocolFields entry
		// still lands on false through the protocolFields branch.
		if b := field.Bool(); b {
			return b, true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if i := int(field.Int()); i != 0 {
			return i, true
		}
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
		// JSON numbers decode into float64 (or json.Number with UseNumber),
		// so accept both forms, not just int.
		switch n := val.(type) {
		case int:
			field.SetInt(int64(n))
		case int64:
			field.SetInt(n)
		case float64:
			field.SetInt(int64(n))
		case json.Number:
			if i, err := n.Int64(); err == nil {
				field.SetInt(i)
			}
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
	*root = filepath.Clean(*root)
	slash := filepath.ToSlash(*root)
	if !strings.HasPrefix(slash, "/var/lib/veil/") {
		// Exactly /var/lib/veil and unrelated paths are invalid: serving the
		// state directory itself would expose state.json, audit/ and backups/
		// to anonymous naive-port visitors (audit #77 F1). The prefix check
		// uses a trailing slash so /var/lib/veilfoo is rejected too (F4).
		if slash == "/var/lib/veil" {
			return errors.New("fallbackRoot must be a subdirectory of /var/lib/veil, not the state directory itself")
		}
		if strings.HasPrefix(slash, "/") {
			return errors.New("fallbackRoot must be within /var/lib/veil")
		}
		*root = filepath.Clean("/var/lib/veil/" + *root)
	}
	// Re-check the boundary after the relative prepend: a "../www" input
	// cleans to /var/lib/www, which escapes /var/lib/veil even though the
	// pre-check saw a relative path (code-review P2, audit #77 F4).
	if !strings.HasPrefix(filepath.ToSlash(*root), "/var/lib/veil/") {
		return errors.New("fallbackRoot must be within /var/lib/veil")
	}
	if filepath.ToSlash(*root) == "/var/lib/veil" {
		return errors.New("fallbackRoot must be a subdirectory of /var/lib/veil, not the state directory itself")
	}
	*root = filepath.ToSlash(*root)
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
