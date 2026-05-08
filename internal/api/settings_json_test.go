package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingsJSONAcceptsLegacyStackButDoesNotMarshalIt(t *testing.T) {
	settings := decodeSettingsJSON(t, `{"panelListen":"127.0.0.1:2096","stack":"both","mode":"server"}`)
	if LegacySettingsStack(settings) != "both" {
		t.Fatalf("legacy stack = %q", LegacySettingsStack(settings))
	}
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if strings.Contains(string(body), `"stack"`) {
		t.Fatalf("settings JSON should not emit legacy stack: %s", body)
	}
}

func TestSettingsJSONRejectsUnknownFields(t *testing.T) {
	var settings Settings
	err := json.Unmarshal([]byte(`{"panelListen":"127.0.0.1:2096","mode":"server","unknown":true}`), &settings)
	if err == nil || !strings.Contains(err.Error(), `json: unknown field "unknown"`) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}
