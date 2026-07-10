package settings

import "testing"

func TestValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	current := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		ProtocolFields: map[string]any{
			"naivePassword":     "old",
			"hysteria2Password": "old-h2",
		},
	}
	update := Settings{
		PanelListen: current.PanelListen,
		Mode:        current.Mode,
		ProtocolFields: map[string]any{
			"naivePassword":     RedactedSecret,
			"hysteria2Password": RedactedSecret,
			"fallbackRoot":      "site",
		},
	}
	if err := NewValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&update, current); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if update.ProtocolFields["naivePassword"] != "old" || update.ProtocolFields["hysteria2Password"] != "old-h2" || update.FallbackRoot != "/var/lib/veil/site" {
		t.Fatalf("settings = %+v", update)
	}
}
