package settings

import "testing"

func TestSettingsRedactionRedactsOnlyPresentSecrets(t *testing.T) {
	settings := NewSettingsRedaction().Redact(Settings{NaivePassword: "naive", Hysteria2Password: "hy", OlcrtcAuth: "olcrtc", Domain: "example.com"})
	if settings.NaivePassword != "[REDACTED]" || settings.Hysteria2Password != "[REDACTED]" || settings.OlcrtcAuth != "[REDACTED]" || settings.Domain != "example.com" {
		t.Fatalf("settings = %+v", settings)
	}
	settings = NewSettingsRedaction().Redact(Settings{})
	if settings.NaivePassword != "" || settings.Hysteria2Password != "" || settings.OlcrtcAuth != "" {
		t.Fatalf("empty secrets should stay empty: %+v", settings)
	}
}
