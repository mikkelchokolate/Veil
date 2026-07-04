package managementstate

import (
	"encoding/json"
	"reflect"
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
	if snapshot.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected SchemaVersion to be upgraded to %d, got %d", CurrentSchemaVersion, snapshot.SchemaVersion)
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

	if snapshot.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected SchemaVersion to migrate to %d, got %d", CurrentSchemaVersion, snapshot.SchemaVersion)
	}
}

func TestDecodeV2MarksExistingAdminSetupComplete(t *testing.T) {
	inputJSON := `{
		"schemaVersion": 2,
		"settings": {
			"panelListen": "127.0.0.1:2096",
			"mode": "server"
		},
		"inbounds": [],
		"routingRules": [],
		"warp": {},
		"users": [
			{"username": "admin", "passwordHash": "hash", "role": "admin"}
		]
	}`

	snapshot, err := NewManagementStateCodec().Decode([]byte(inputJSON))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snapshot.SchemaVersion != CurrentSchemaVersion || !snapshot.Setup.Completed {
		t.Fatalf("expected migrated setup completion, got %+v", snapshot)
	}
}

func TestDecodeV3MigratesProtocolFields(t *testing.T) {
	inputJSON := `{
		"schemaVersion": 3,
		"settings": {
			"panelListen": "127.0.0.1:2096",
			"mode": "server",
			"naiveUsername": "veil",
			"naivePassword": "secret",
			"hysteria2Password": "hy-secret",
			"masqueradeURL": "https://example.com",
			"fallbackRoot": "/var/lib/veil/www",
			"olcrtcAuth": "jitsi",
			"olcrtcTransport": "datachannel",
			"olcrtcRoomID": "room-1"
		},
		"inbounds": [
			{
				"name": "naive",
				"protocol": "naiveproxy",
				"transport": "tcp",
				"port": 443,
				"enabled": true,
				"naiveUsername": "u",
				"naivePassword": "p",
				"fallbackRoot": "/var/www"
			}
		],
		"routingRules": [],
		"warp": {},
		"users": []
	}`

	snapshot, err := NewManagementStateCodec().Decode([]byte(inputJSON))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snapshot.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentSchemaVersion, snapshot.SchemaVersion)
	}

	wantSettingsPF := map[string]any{
		"naiveUsername":     "veil",
		"naivePassword":     "secret",
		"hysteria2Password": "hy-secret",
		"masqueradeURL":     "https://example.com",
		"fallbackRoot":      "/var/lib/veil/www",
		"olcrtcAuth":        "jitsi",
		"olcrtcTransport":   "datachannel",
		"olcrtcRoomID":      "room-1",
	}
	if !reflect.DeepEqual(snapshot.Settings.ProtocolFields, wantSettingsPF) {
		t.Errorf("settings protocolFields = %+v, want %+v", snapshot.Settings.ProtocolFields, wantSettingsPF)
	}
	if snapshot.Settings.NaiveUsername != "" {
		t.Errorf("expected legacy naiveUsername to be cleared, got %q", snapshot.Settings.NaiveUsername)
	}

	if len(snapshot.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(snapshot.Inbounds))
	}
	wantInboundPF := map[string]any{
		"naiveUsername": "u",
		"naivePassword": "p",
		"fallbackRoot":  "/var/www",
	}
	if !reflect.DeepEqual(snapshot.Inbounds[0].ProtocolFields, wantInboundPF) {
		t.Errorf("inbound protocolFields = %+v, want %+v", snapshot.Inbounds[0].ProtocolFields, wantInboundPF)
	}
}

func TestDecodeV2WithoutAdminMarksSetupIncomplete(t *testing.T) {
	inputJSON := `{
		"schemaVersion": 2,
		"settings": {"panelListen": "127.0.0.1:2096", "mode": "server"},
		"inbounds": [],
		"routingRules": [],
		"warp": {},
		"users": [{"username": "viewer", "passwordHash": "hash", "role": "viewer"}]
	}`

	snapshot, err := NewManagementStateCodec().Decode([]byte(inputJSON))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snapshot.SchemaVersion != CurrentSchemaVersion || snapshot.Setup.Completed {
		t.Fatalf("expected incomplete setup, got %+v", snapshot)
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
