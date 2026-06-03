package settings

import "testing"

func TestSettingsValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", NaivePassword: "[REDACTED]", Hysteria2Password: "[REDACTED]", OlcrtcAuth: "[REDACTED]", FallbackRoot: "www"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{NaivePassword: "old-naive", Hysteria2Password: "old-hy", OlcrtcAuth: "old-olcrtc"})
	if err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.NaivePassword != "old-naive" || settings.Hysteria2Password != "old-hy" || settings.OlcrtcAuth != "old-olcrtc" {
		t.Fatalf("secrets = %+v", settings)
	}
	if settings.FallbackRoot != "/var/lib/veil/www" {
		t.Fatalf("fallbackRoot = %q", settings.FallbackRoot)
	}
}

func TestSettingsValidationRejectsCaddyPanelAccessWithoutDomainAndEmail(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "--domain and --email are required for caddy Panel access" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationRejectsCaddyPanelAccessWithoutWebBasePath(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", Mode: "server"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
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
		err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
		if err == nil || err.Error() != tc.errStr {
			t.Errorf("listen %q: expected error %q, got %v", tc.listen, tc.errStr, err)
		}
	}
}
