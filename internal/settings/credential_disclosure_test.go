package settings

import "testing"

func TestCredentialDisclosureRedactsAndPreservesSecrets(t *testing.T) {
	disclosure := NewCredentialDisclosure()
	if disclosure.Redact("secret") != RedactedSecret || disclosure.Redact("") != "" {
		t.Fatalf("redact failed")
	}
	if got := disclosure.PreserveRedacted(RedactedSecret, "old-secret"); got != "old-secret" {
		t.Fatalf("preserve redacted = %q", got)
	}
	if got := disclosure.PreserveRedacted("new-secret", "old-secret"); got != "new-secret" {
		t.Fatalf("preserve new = %q", got)
	}
}

func TestCredentialDisclosureRedactsKnownSecretsInText(t *testing.T) {
	got := NewCredentialDisclosure().RedactText("a secret b token", []string{"secret", "", "token"})
	if got != "a [REDACTED] b [REDACTED]" {
		t.Fatalf("redacted text = %q", got)
	}
}
