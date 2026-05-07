package api

import "testing"

func TestSettingsValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: "server", NaivePassword: "[REDACTED]", Hysteria2Password: "[REDACTED]", FallbackRoot: "www"}
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

func TestSettingsValidationRejectsInvalidStack(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Stack: "bad", Mode: "server"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "stack must be panel, mieru, naive, hysteria2, or both" {
		t.Fatalf("err = %v", err)
	}
}
