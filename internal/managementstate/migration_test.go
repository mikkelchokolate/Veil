package managementstate

import (
	"encoding/json"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestDecodeWithLegacyFieldMigration(t *testing.T) {
	// JSON representing state version 1 with a "legacyField" that has been removed from the Go struct.
	// This would trigger a DisallowUnknownFields error if migration is not applied.
	inputJSON := `{
		"schemaVersion": 1,
		"legacyField": "deprecated_value",
		"settings": {
			"panelListen": "127.0.0.1:2096",
			"mode": "server"
		},
		"inbounds": [],
		"routingRules": [],
		"warp": {
			"enabled": false,
			"endpoint": "engage.cloudflareclient.com:2408"
		}
	}`

	codec := NewManagementStateCodec()
	snapshot, err := codec.Decode([]byte(inputJSON))
	if err != nil {
		t.Fatalf("unexpected error decoding legacy state: %v", err)
	}

	// Verify the decoded snapshot has the updated schema version
	if snapshot.SchemaVersion != 2 {
		t.Errorf("expected SchemaVersion to be upgraded to 2, got %d", snapshot.SchemaVersion)
	}

	// Verify the settings were parsed correctly
	if snapshot.Settings.PanelListen != "127.0.0.1:2096" {
		t.Errorf("expected panelListen to be '127.0.0.1:2096', got %q", snapshot.Settings.PanelListen)
	}
}

func TestDecodeWithMissingVersionDefaultsToV1(t *testing.T) {
	// JSON representing state without schemaVersion field (should default to 1 and migrate to 2)
	inputJSON := `{
		"legacyField": "deprecated_value",
		"settings": {
			"panelListen": "127.0.0.1:2096",
			"mode": "server"
		},
		"inbounds": [],
		"routingRules": [],
		"warp": {
			"enabled": false,
			"endpoint": "engage.cloudflareclient.com:2408"
		}
	}`

	codec := NewManagementStateCodec()
	snapshot, err := codec.Decode([]byte(inputJSON))
	if err != nil {
		t.Fatalf("unexpected error decoding unversioned state: %v", err)
	}

	if snapshot.SchemaVersion != 2 {
		t.Errorf("expected SchemaVersion to migrate to 2, got %d", snapshot.SchemaVersion)
	}
}

func TestEncodeSavesLatestSchemaVersion(t *testing.T) {
	codec := NewManagementStateCodec()
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen: "127.0.0.1:2096",
			Mode:        "server",
		},
	}

	encoded, err := codec.Encode(snapshot)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	// Decode back generically to check schemaVersion field
	var raw map[string]interface{}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("failed to parse encoded json: %v", err)
	}

	val, ok := raw["schemaVersion"]
	if !ok {
		t.Fatal("expected schemaVersion to be present in encoded output")
	}

	num, ok := val.(float64)
	if !ok || int(num) != CurrentSchemaVersion {
		t.Errorf("expected schemaVersion in JSON to be %d, got %v", CurrentSchemaVersion, val)
	}
}
