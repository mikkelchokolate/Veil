package managementstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mikkelchokolate/Veil/internal/model"
)

const CurrentSchemaVersion = 2

// migrations maps a starting version to a function that upgrades the raw state map to the next version.
var migrations = map[int]func(map[string]interface{}) (map[string]interface{}, error){
	1: func(raw map[string]interface{}) (map[string]interface{}, error) {
		// Migration from version 1 to 2.
		// Removes legacy obsolete fields if they exist to prevent DisallowUnknownFields from failing.
		delete(raw, "legacyField")
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
