package settings

import "testing"

func TestSettingsValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		ProtocolFields: map[string]any{
			"naivePassword":     RedactedSecret,
			"hysteria2Password": RedactedSecret,
			"fallbackRoot":      "www",
		},
	}
	current := Settings{
		ProtocolFields: map[string]any{
			"naivePassword":     "old-naive",
			"hysteria2Password": "old-hy",
		},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, current)
	if err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.ProtocolFields["naivePassword"] != "old-naive" || settings.ProtocolFields["hysteria2Password"] != "old-hy" {
		t.Fatalf("secrets = %+v", settings)
	}
	if settings.ProtocolFields["fallbackRoot"] != "/var/lib/veil/www" {
		t.Fatalf("fallbackRoot = %q", settings.ProtocolFields["fallbackRoot"])
	}
}

func TestSettingsValidationRejectsCaddyPanelAccessWithoutDomainAndEmail(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server"}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "--domain and --email are required for caddy Panel access" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationRejectsCaddyPanelAccessWithoutWebBasePath(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", Mode: "server"}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "webBasePath is required for caddy Panel access" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationRejectsInvalidPanelListenPorts(t *testing.T) {
	tests := []struct {
		listen string
		errStr string
	}{
		{"127.0.0.1:abc", "panelListen port must be a valid integer between 1 and 65535"},
		{"127.0.0.1:0", "panelListen port must be a valid integer between 1 and 65535"},
		{"127.0.0.1:70000", "panelListen port must be a valid integer between 1 and 65535"},
		{"127.0.0.1:-5", "panelListen port must be a valid integer between 1 and 65535"},
		{"127.0.0.1", "panelListen must be host:port"},
	}

	for _, tc := range tests {
		settings := Settings{PanelListen: tc.listen, Mode: "server"}
		err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
		if err == nil || err.Error() != tc.errStr {
			t.Errorf("listen %q: expected error %q, got %v", tc.listen, tc.errStr, err)
		}
	}
}
