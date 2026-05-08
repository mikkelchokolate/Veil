package api

import (
	"encoding/json"
	"testing"
)

func TestSettingsValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", NaivePassword: "[REDACTED]", Hysteria2Password: "[REDACTED]", FallbackRoot: "www"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{NaivePassword: "old-naive", Hysteria2Password: "old-hy"})
	if err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.NaivePassword != "old-naive" || settings.Hysteria2Password != "old-hy" {
		t.Fatalf("secrets = %+v", settings)
	}
	if settings.FallbackRoot != "/var/lib/veil/www" {
		t.Fatalf("fallbackRoot = %q", settings.FallbackRoot)
	}
}

func TestSettingsValidationAcceptsLegacyProtocolStackSelection(t *testing.T) {
	for _, legacyStack := range []string{"both", "naive", "hysteria2", "mieru"} {
		t.Run(legacyStack, func(t *testing.T) {
			settings := decodeSettingsJSON(t, `{"panelListen":"127.0.0.1:2096","stack":"`+legacyStack+`","mode":"server"}`)
			if err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{}); err != nil {
				t.Fatalf("legacy protocol stack should migrate to Panel Inbounds compatibility: %v", err)
			}
			if LegacySettingsStack(settings) != legacyStack {
				t.Fatalf("legacy stack compatibility was not retained for validation, got %q", LegacySettingsStack(settings))
			}
		})
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

func TestSettingsValidationRejectsUnknownStack(t *testing.T) {
	settings := decodeSettingsJSON(t, `{"panelListen":"127.0.0.1:2096","stack":"bad","mode":"server"}`)
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "stack must be panel; protocols are configured as Panel inbounds" {
		t.Fatalf("err = %v", err)
	}
}

func decodeSettingsJSON(t *testing.T, body string) Settings {
	t.Helper()
	var settings Settings
	if err := json.Unmarshal([]byte(body), &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return settings
}
