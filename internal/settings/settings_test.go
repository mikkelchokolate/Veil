package settings

import "testing"

func TestValidationPreservesRedactedSecretsAndNormalizesFallbackRoot(t *testing.T) {
	current := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", NaivePassword: "old", Hysteria2Password: "old-h2"}
	update := current
	update.NaivePassword = RedactedSecret
	update.Hysteria2Password = RedactedSecret
	update.FallbackRoot = "site"
	if err := NewValidation().NormalizeAndValidate(&update, current); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if update.NaivePassword != "old" || update.Hysteria2Password != "old-h2" || update.FallbackRoot != "/var/lib/veil/site" {
		t.Fatalf("settings = %+v", update)
	}
}
