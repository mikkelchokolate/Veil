package settings

import "testing"

func TestNewRedactionReturnsNonZeroRedaction(t *testing.T) {
	r := NewRedaction()
	settings := r.Redact(Settings{NaivePassword: "secret"})
	if settings.NaivePassword != RedactedSecret {
		t.Fatalf("expected redacted password, got %q", settings.NaivePassword)
	}
}

func TestNewValidationReturnsNonZeroValidation(t *testing.T) {
	v := NewValidation()
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}
	if err := v.NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
}
