package managementstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

const CurrentSchemaVersion = 4

// settingsProtocolFieldKeys are legacy flat settings fields that are now kept
// inside settings.protocolFields for dynamic UI/validation. They are derived
// from the registered protocol plugins so the migration stays consistent with
// the plugin architecture.
var settingsProtocolFieldKeys = protocolFieldKeys(protocols.NewRegistry().SettingsFieldSchemas())

// inboundProtocolFieldKeys are legacy flat inbound fields that are now kept
// inside inbound.protocolFields for dynamic UI/validation. They are derived
// from the registered protocol plugins so the migration stays consistent with
// the plugin architecture.
var inboundProtocolFieldKeys = protocolFieldKeys(protocols.NewRegistry().InboundFieldSchemas())

func protocolFieldKeys(schemas []schema.FieldSchema) []string {
	keys := make([]string, 0, len(schemas))
	seen := make(map[string]struct{}, len(schemas))
	for _, f := range schemas {
		if _, ok := seen[f.Key]; ok {
			continue
		}
		seen[f.Key] = struct{}{}
		keys = append(keys, f.Key)
	}
	return keys
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
