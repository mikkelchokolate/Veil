package api

import "testing"

func TestSettingsManagementPreservesRedactedPasswordsAndSaves(t *testing.T) {
	settings := Settings{
		PanelListen:       "127.0.0.1:2096",
		Stack:             "both",
		Mode:              "dev",
		NaivePassword:     "naive-secret",
		Hysteria2Password: "hy2-secret",
	}
	saves := 0
	management := NewSettingsManagement(&settings, func() error {
		saves++
		return nil
	})

	updated, err := management.Update(Settings{
		PanelListen:       "127.0.0.1:2096",
		Stack:             "naive",
		Mode:              "dev",
		NaivePassword:     "[REDACTED]",
		Hysteria2Password: "[REDACTED]",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.NaivePassword != "[REDACTED]" || updated.Hysteria2Password != "[REDACTED]" {
		t.Fatalf("response should redact passwords: %+v", updated)
	}
	if settings.NaivePassword != "naive-secret" || settings.Hysteria2Password != "hy2-secret" {
		t.Fatalf("stored passwords not preserved: %+v", settings)
	}
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}
}

func TestSettingsManagementRejectsInvalidSettingsWithoutSave(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: "dev"}
	saves := 0
	management := NewSettingsManagement(&settings, func() error {
		saves++
		return nil
	})

	_, err := management.Update(Settings{PanelListen: "bad-listen", Stack: "bad", Mode: "dev"})
	if err == nil {
		t.Fatalf("expected invalid settings error")
	}
	if saves != 0 {
		t.Fatalf("saves = %d, want 0", saves)
	}
}
