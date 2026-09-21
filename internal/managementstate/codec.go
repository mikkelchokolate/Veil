package managementstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
)

const CurrentSchemaVersion = 5

// settingsProtocolFieldKeys are legacy flat settings fields that are now kept
// inside settings.protocolFields for dynamic UI/validation.
var settingsProtocolFieldKeys = []string{
	"naiveUsername", "naivePassword",
	"hysteria2Password", "hysteria2Insecure", "masqueradeURL",
	"fallbackRoot",
	"olcrtcAuth", "olcrtcTransport", "olcrtcRoomID",
}

// inboundProtocolFieldKeys are legacy flat inbound fields that are now kept
// inside inbound.protocolFields for dynamic UI/validation.
var inboundProtocolFieldKeys = []string{
	"naiveUsername", "naivePassword",
	"hysteria2Password", "hysteria2Insecure", "masqueradeURL",
	"fallbackRoot",
	"olcrtcAuth", "olcrtcTransport", "olcrtcRoomID",
}

func moveToProtocolFields(obj map[string]interface{}, keys []string) map[string]interface{} {
	pf, ok := obj["protocolFields"].(map[string]interface{})
	if !ok {
		pf = make(map[string]interface{})
	}
	for _, key := range keys {
		if val, exists := obj[key]; exists {
			pf[key] = val
			delete(obj, key)
		}
	}
	if len(pf) > 0 {
		obj["protocolFields"] = pf
	}
	return obj
}

// migrations maps a starting version to a function that upgrades the raw state map to the next version.
var migrations = map[int]func(map[string]interface{}) (map[string]interface{}, error){
	1: func(raw map[string]interface{}) (map[string]interface{}, error) {
		// Migration from version 1 to 2.
		// Removes legacy obsolete fields if they exist to prevent DisallowUnknownFields from failing.
		delete(raw, "legacyField")
		return raw, nil
	},
	2: func(raw map[string]interface{}) (map[string]interface{}, error) {
		completed := false
		if users, ok := raw["users"].([]interface{}); ok {
			for _, entry := range users {
				user, ok := entry.(map[string]interface{})
				if ok && user["role"] == "admin" {
					completed = true
					break
				}
			}
		}
		raw["setup"] = map[string]interface{}{"completed": completed}
		return raw, nil
	},
	3: func(raw map[string]interface{}) (map[string]interface{}, error) {
		// Migration from version 3 to 4.
		// Collapse legacy protocol-specific flat fields into protocolFields so
		// the dynamic Panel UI and plugin validators can consume them uniformly.
		if settings, ok := raw["settings"].(map[string]interface{}); ok {
			raw["settings"] = moveToProtocolFields(settings, settingsProtocolFieldKeys)
		}
		if inbounds, ok := raw["inbounds"].([]interface{}); ok {
			for i, entry := range inbounds {
				if inbound, ok := entry.(map[string]interface{}); ok {
					inbounds[i] = moveToProtocolFields(inbound, inboundProtocolFieldKeys)
				}
			}
		}
		return raw, nil
	},
	4: func(raw map[string]interface{}) (map[string]interface{}, error) {
		// Migration from version 4 to 5.
		// The naive fallback root moved from /var/lib/veil/www to <etc>/www
		// when veil-caddy.service switched to veil-proxy with /var/lib/veil in
		// InaccessiblePaths. Stored roots under /var/lib/veil are dead
		// configuration the renderer now rejects, so rewrite them here:
		// /var/lib/veil/www and other var-lib roots fall back to the managed
		// default; /var/lib/veil/www/<sub> survives as a relative root so it
		// resolves under whichever <etc>/www the install uses (issue #618).
		if settings, ok := raw["settings"].(map[string]interface{}); ok {
			migrateFallbackRootValue(settings)
		}
		if inbounds, ok := raw["inbounds"].([]interface{}); ok {
			for _, entry := range inbounds {
				if inbound, ok := entry.(map[string]interface{}); ok {
					migrateFallbackRootValue(inbound)
				}
			}
		}
		return raw, nil
	},
}

// migrateFallbackRootValue rewrites a legacy /var/lib/veil fallbackRoot on a
// settings or inbound object, covering both the flat field and the
// protocolFields map the v3→v4 migration produces.
func migrateFallbackRootValue(obj map[string]interface{}) {
	rewrite := func(m map[string]interface{}, key string) {
		value, ok := m[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return
		}
		if migrated, keep := migratedFallbackRoot(value); keep {
			m[key] = migrated
		} else {
			delete(m, key)
		}
	}
	rewrite(obj, "fallbackRoot")
	if pf, ok := obj["protocolFields"].(map[string]interface{}); ok {
		rewrite(pf, "fallbackRoot")
	}
}

// migratedFallbackRoot maps a stored fallbackRoot onto the post-/var/lib/veil
// contract. It returns (value, false) when the key should be removed so the
// managed <etc>/www default applies, and (relative, true) for subtrees of the
// legacy www root so they resolve under the managed tree of whatever etc dir
// the install uses. Values outside /var/lib/veil are left untouched.
func migratedFallbackRoot(value string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	const legacyWWW = "/var/lib/veil/www"
	switch {
	case clean == legacyWWW:
		// The packaged old root maps onto the packaged new default; dropping
		// the key lets the renderer resolve <etc>/www for this install.
		return "", false
	case strings.HasPrefix(clean, legacyWWW+"/"):
		return strings.TrimPrefix(clean, legacyWWW+"/"), true
	case clean == "/var/lib/veil" || strings.HasPrefix(clean, "/var/lib/veil/"):
		// Other var-lib roots have no valid destination: unreachable to Caddy
		// and rejected by validation. Drop them so the managed default applies.
		return "", false
	}
	return value, true
}

type ManagementStateCodec struct{}

func NewManagementStateCodec() ManagementStateCodec { return ManagementStateCodec{} }

func (ManagementStateCodec) Decode(body []byte) (model.ManagementSnapshot, error) {
	// Parse generic map to check and migrate schema version
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err == nil && raw != nil {
		schemaVersion := 1
		if val, ok := raw["schemaVersion"]; ok {
			if num, ok := val.(float64); ok {
				schemaVersion = int(num)
			}
		}

		if schemaVersion > CurrentSchemaVersion {
			// A state file from a newer build would be silently overwritten with
			// CurrentSchemaVersion on the next Save, discarding future fields.
			// Refuse to load it instead of downgrading.
			return model.ManagementSnapshot{}, fmt.Errorf("state schema version %d is newer than supported version %d", schemaVersion, CurrentSchemaVersion)
		}

		migrated := false
		for schemaVersion < CurrentSchemaVersion {
			migrateFn, ok := migrations[schemaVersion]
			if !ok {
				return model.ManagementSnapshot{}, fmt.Errorf("no state migration registered for version %d", schemaVersion)
			}
			newRaw, err := migrateFn(raw)
			if err != nil {
				return model.ManagementSnapshot{}, fmt.Errorf("state migration v%d failed: %w", schemaVersion, err)
			}
			raw = newRaw
			schemaVersion++
			raw["schemaVersion"] = schemaVersion
			migrated = true
		}

		if migrated {
			newBody, err := json.Marshal(raw)
			if err != nil {
				return model.ManagementSnapshot{}, fmt.Errorf("failed to marshal migrated state: %w", err)
			}
			body = newBody
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var snapshot model.ManagementSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return model.ManagementSnapshot{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return model.ManagementSnapshot{}, errors.New("state body must contain a single JSON value")
		}
		return model.ManagementSnapshot{}, err
	}
	// Install and admin-reset historically wrote an admin user without flipping
	// setup.completed. Heal that in-memory so first-run status matches login.
	CompleteSetupForAdmins(&snapshot, time.Time{})
	return snapshot, nil
}

func (ManagementStateCodec) Encode(snapshot model.ManagementSnapshot) ([]byte, error) {
	snapshot.SchemaVersion = CurrentSchemaVersion
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func DecodeError(err error) error {
	if err == nil {
		return nil
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
