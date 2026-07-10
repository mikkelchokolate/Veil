package settings

import "testing"

func TestSettingsRedactionRedactsOnlyPresentSecrets(t *testing.T) {
	settings := NewSettingsRedactionWithFieldSchemas(testSettingsFieldSchemas()).Redact(Settings{
		Domain: "example.com",
		ProtocolFields: map[string]any{
			"naivePassword":     "naive",
			"hysteria2Password": "hy",
			"olcrtcAuth":        "jitsi",
		},
	})
	if settings.ProtocolFields["naivePassword"] != RedactedSecret ||
		settings.ProtocolFields["hysteria2Password"] != RedactedSecret {
		t.Fatalf("passwords were not redacted: %+v", settings)
	}
	if settings.ProtocolFields["olcrtcAuth"] != "jitsi" {
		t.Fatalf("non-secret field was redacted: %+v", settings)
	}
	if settings.Domain != "example.com" {
		t.Fatalf("domain was changed: %+v", settings)
	}
	settings = NewSettingsRedactionWithFieldSchemas(testSettingsFieldSchemas()).Redact(Settings{})
	if settings.ProtocolFields != nil {
		t.Fatalf("empty secrets should stay empty: %+v", settings)
	}
}
