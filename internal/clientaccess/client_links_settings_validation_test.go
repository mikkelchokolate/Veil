package clientaccess

import "testing"

func TestClientLinksSettingsValidationAllowsDomainlessLocalPanel(t *testing.T) {
	validator := NewClientLinksSettingsValidation()
	if err := validator.Validate(Settings{Domain: "example.com"}); err != nil {
		t.Fatalf("Validate valid: %v", err)
	}
	if err := validator.Validate(Settings{Domain: "  "}); err != nil {
		t.Fatalf("domainless direct/local Panel should not fail client links validation: %v", err)
	}
}
