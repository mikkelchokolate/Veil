package managementstate

import (
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestCodecDecodeRejectsInvalidJSON(t *testing.T) {
	codec := NewManagementStateCodec()
	_, err := codec.Decode([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if syntaxErr := DecodeError(err); syntaxErr == nil {
		t.Fatalf("expected DecodeError to wrap syntax error, got %v", err)
	}
}

func TestCodecDecodeRejectsUnexpectedEOF(t *testing.T) {
	codec := NewManagementStateCodec()
	_, err := codec.Decode([]byte(`{"settings":`))
	if err == nil {
		t.Fatal("expected error for unexpected EOF")
	}
	if syntaxErr := DecodeError(err); syntaxErr == nil {
		t.Fatalf("expected DecodeError to wrap unexpected EOF, got %v", err)
	}
}

func TestCodecDecodeRejectsMultipleJSONValues(t *testing.T) {
	codec := NewManagementStateCodec()
	body := []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}{}`)
	_, err := codec.Decode(body)
	if err == nil || err.Error() != "state body must contain a single JSON value" {
		t.Fatalf("expected multiple JSON values error, got %v", err)
	}
}

func TestCodecDecodeNoMigrationNeededForCurrentVersion(t *testing.T) {
	codec := NewManagementStateCodec()
	body := []byte(`{
		"schemaVersion":4,
		"settings":{"panelListen":"127.0.0.1:2096","mode":"server"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)
	snapshot, err := codec.Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snapshot.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", snapshot.SchemaVersion, CurrentSchemaVersion)
	}
}

func TestCodecDecodeFailsForUnknownSchemaVersion(t *testing.T) {
	// schemaVersion 0 has no registered migration and is less than CurrentSchemaVersion.
	codec := NewManagementStateCodec()
	body := []byte(`{"schemaVersion":0,"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`)
	_, err := codec.Decode(body)
	if err == nil {
		t.Fatal("expected error for unknown schema version")
	}
	if err.Error() != "no state migration registered for version 0" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodecDecodeFailsWhenMigrationReturnsError(t *testing.T) {
	// Register a temporary failing migration for schema version 0.
	migrations[0] = func(map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("boom")
	}
	defer delete(migrations, 0)

	codec := NewManagementStateCodec()
	body := []byte(`{"schemaVersion":0,"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`)
	_, err := codec.Decode(body)
	if err == nil {
		t.Fatal("expected migration failed error")
	}
	if err.Error() != "state migration v0 failed: boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodecDecodeErrorHandlesCases(t *testing.T) {
	if DecodeError(nil) != nil {
		t.Fatalf("DecodeError(nil) = %v", DecodeError(nil))
	}
	if DecodeError(errors.New("plain")) != nil {
		t.Fatalf("DecodeError(plain) = %v", DecodeError(errors.New("plain")))
	}
	if DecodeError(io.ErrUnexpectedEOF) == nil {
		t.Fatal("DecodeError(io.ErrUnexpectedEOF) returned nil")
	}
}

func TestCodecDecodeNullBodySkipsMigration(t *testing.T) {
	codec := NewManagementStateCodec()
	snapshot, err := codec.Decode([]byte(`null`))
	if err != nil {
		t.Fatalf("Decode null: %v", err)
	}
	if snapshot.SchemaVersion != 0 {
		t.Fatalf("expected zero snapshot, got %+v", snapshot)
	}
}

func TestCodecDecodeEmptyObjectMigratesFromV1(t *testing.T) {
	// An empty JSON object has no schemaVersion, so it defaults to v1 and migrates.
	codec := NewManagementStateCodec()
	body := []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"},"inbounds":[],"routingRules":[],"warp":{"enabled":false}}`)
	snapshot, err := codec.Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snapshot.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version = %d", snapshot.SchemaVersion)
	}
}

func TestCodecDecodeReturnsSecondDecodeError(t *testing.T) {
	// First JSON value decodes cleanly, trailing incomplete value causes a second decode error.
	codec := NewManagementStateCodec()
	_, err := codec.Decode([]byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}} {`))
	if err == nil {
		t.Fatal("expected error on second decode")
	}
}

func TestCodecEncodeHandlesMarshalError(t *testing.T) {
	codec := NewManagementStateCodec()
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{ProtocolFields: map[string]any{"ch": make(chan int)}},
	}
	_, err := codec.Encode(snapshot)
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestCodecDecodeMigrationMarshalFailure(t *testing.T) {
	migrations[0] = func(raw map[string]interface{}) (map[string]interface{}, error) {
		raw["bad"] = make(chan int)
		return raw, nil
	}
	defer delete(migrations, 0)

	codec := NewManagementStateCodec()
	body := []byte(`{"schemaVersion":0,"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`)
	_, err := codec.Decode(body)
	if err == nil {
		t.Fatal("expected marshal error after migration")
	}
}

func TestCodecEncodeSetsSchemaVersionAndTrailingNewline(t *testing.T) {
	codec := NewManagementStateCodec()
	body, err := codec.Encode(model.ManagementSnapshot{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if body[len(body)-1] != '\n' {
		t.Fatal("expected trailing newline in encoded output")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if int(raw["schemaVersion"].(float64)) != CurrentSchemaVersion {
		t.Fatalf("schemaVersion = %v", raw["schemaVersion"])
	}
}

func TestCodecMoveToProtocolFieldsCreatesNewMap(t *testing.T) {
	obj := map[string]interface{}{
		"naiveUsername": "veil",
		"fallbackRoot":  "/var/www",
	}
	result := moveToProtocolFields(obj, settingsProtocolFieldKeys)
	pf, ok := result["protocolFields"].(map[string]interface{})
	if !ok || pf["naiveUsername"] != "veil" {
		t.Fatalf("protocolFields not created: %+v", result)
	}
	if _, exists := obj["naiveUsername"]; exists {
		t.Fatal("legacy key not deleted")
	}
}

func TestCodecMoveToProtocolFieldsMergesExistingFields(t *testing.T) {
	obj := map[string]interface{}{
		"protocolFields": map[string]interface{}{"existing": "value"},
		"naiveUsername":  "veil",
	}
	result := moveToProtocolFields(obj, settingsProtocolFieldKeys)
	pf := result["protocolFields"].(map[string]interface{})
	if pf["existing"] != "value" || pf["naiveUsername"] != "veil" {
		t.Fatalf("existing protocolFields not preserved: %+v", pf)
	}
}

func TestCodecMoveToProtocolFieldsLeavesEmptyObjectUntouched(t *testing.T) {
	obj := map[string]interface{}{"other": "value"}
	result := moveToProtocolFields(obj, settingsProtocolFieldKeys)
	if _, exists := result["protocolFields"]; exists {
		t.Fatal("expected no protocolFields when no keys moved")
	}
}
