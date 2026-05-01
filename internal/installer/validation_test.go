package installer

import "testing"

func TestValidateDomainAcceptsHostname(t *testing.T) {
	if err := ValidateDomain("vpn.example.com"); err != nil {
		t.Fatalf("expected valid domain: %v", err)
	}
}

func TestValidateDomainRejectsInvalidValues(t *testing.T) {
	for _, domain := range []string{"", "localhost", "bad domain.com", "https://example.com", "-bad.example.com", "bad-.example.com"} {
		t.Run(domain, func(t *testing.T) {
			if err := ValidateDomain(domain); err == nil {
				t.Fatalf("expected invalid domain error")
			}
		})
	}
}

func TestValidateEmailAcceptsEmail(t *testing.T) {
	if err := ValidateEmail("admin@example.com"); err != nil {
		t.Fatalf("expected valid email: %v", err)
	}
}

func TestValidateEmailRejectsInvalidValues(t *testing.T) {
	for _, email := range []string{"", "admin", "admin@", "@example.com"} {
		t.Run(email, func(t *testing.T) {
			if err := ValidateEmail(email); err == nil {
				t.Fatalf("expected invalid email error")
			}
		})
	}
}
