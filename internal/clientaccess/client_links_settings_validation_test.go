package clientaccess

import "testing"

func TestClientLinksSettingsValidationRequiresDomain(t *testing.T) {
	validator := NewClientLinksSettingsValidation()
	if err := validator.Validate(Settings{Domain: "example.com"}); err != nil {
		t.Fatalf("Validate valid: %v", err)
	}
	if err := validator.Validate(Settings{Domain: "  "}); err == nil || err.Error() != "domain is required to build client links" {
		t.Fatalf("err = %v", err)
	}
}
