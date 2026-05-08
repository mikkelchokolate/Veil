package api

import "testing"

func TestSettingsValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", NaivePassword: "[REDACTED]", Hysteria2Password: "[REDACTED]", FallbackRoot: "www"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{NaivePassword: "old-naive", Hysteria2Password: "old-hy"})
	if err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.Stack != "panel" {
		t.Fatalf("legacy empty stack should normalize to panel, got %q", settings.Stack)
	}
	if settings.NaivePassword != "old-naive" || settings.Hysteria2Password != "old-hy" {
		t.Fatalf("secrets = %+v", settings)
	}
	if settings.FallbackRoot != "/var/lib/veil/www" {
		t.Fatalf("fallbackRoot = %q", settings.FallbackRoot)
	}
}

func TestSettingsValidationNormalizesLegacyProtocolStackSelection(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: "server"}
	if err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("legacy protocol stack should migrate to panel: %v", err)
	}
	if settings.Stack != "panel" {
		t.Fatalf("legacy protocol stack should not persist as both, got %q", settings.Stack)
	}
}

func TestSettingsValidationRejectsCaddyPanelAccessWithoutWebBasePath(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", Stack: "panel", Mode: "server"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "webBasePath is required for caddy Panel access" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationRejectsUnknownStack(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Stack: "bad", Mode: "server"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "stack must be panel; protocols are configured as Panel inbounds" {
		t.Fatalf("err = %v", err)
	}
}
