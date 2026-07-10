package settings

import "testing"

func TestNewRedactionReturnsNonZeroRedaction(t *testing.T) {
	r := NewRedactionWithFieldSchemas(testSettingsFieldSchemas())
	settings := r.Redact(Settings{ProtocolFields: map[string]any{"naivePassword": "secret"}})
	if settings.ProtocolFields["naivePassword"] != RedactedSecret {
		t.Fatalf("expected redacted password, got %q", settings.ProtocolFields["naivePassword"])
	}
}

func TestNewValidationReturnsNonZeroValidation(t *testing.T) {
	v := NewValidationWithFieldSchemas(testSettingsFieldSchemas())
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}
	if err := v.NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
}
