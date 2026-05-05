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

func TestValidateDomainRejectsDomainTooLong(t *testing.T) {
	label50 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	domain := label50 + "." + label50 + "." + label50 + "." + label50 + "." + label50 + ".com"
	if err := ValidateDomain(domain); err == nil {
		t.Fatalf("expected domain too long error")
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

func TestValidateDomainRejectsSpecialCharacters(t *testing.T) {
	for _, domain := range []string{
		"example\n.com",
		"example\r.com",
		"example\t.com",
		"example\x00.com",
		"exa mple.com",
		"exa/mple.com",
		"exa_mple.com",
	} {
		t.Run("special="+domain, func(t *testing.T) {
			if err := ValidateDomain(domain); err == nil {
				t.Fatalf("expected invalid domain error for %q", domain)
			}
		})
	}
}

func TestValidateDomainRejectsLeadingHyphenInAnyLabel(t *testing.T) {
	for _, domain := range []string{
		"-example.com",
		"abc.-def.com",
		"abc.def-.com",
	} {
		t.Run(domain, func(t *testing.T) {
			if err := ValidateDomain(domain); err == nil {
				t.Fatalf("expected invalid domain error for %q", domain)
			}
		})
	}
}

func TestValidateDomainAcceptsLabelAtMaxLength(t *testing.T) {
	// 63-char label is valid
	label63 := ""
	for i := 0; i < 63; i++ {
		label63 += "a"
	}
	domain := label63 + ".example.com"
	if err := ValidateDomain(domain); err != nil {
		t.Fatalf("expected valid domain with 63-char label: %v", err)
	}
}

func TestValidateDomainAcceptsHyphenInMiddleOfLabel(t *testing.T) {
	domain := "my-vpn.example-server.com"
	if err := ValidateDomain(domain); err != nil {
		t.Fatalf("expected valid domain with hyphens: %v", err)
	}
}

func TestValidateEmailRejectsNewlinesAndSpecialChars(t *testing.T) {
	for _, email := range []string{
		"admin\n@example.com",
		"admin\r@example.com",
		"admin\x00@example.com",
		"admin @example.com",
	} {
		t.Run("special="+email, func(t *testing.T) {
			if err := ValidateEmail(email); err == nil {
				t.Fatalf("expected invalid email error for %q", email)
			}
		})
	}
}

func TestValidateEmailRejectsDisplayNameFormat(t *testing.T) {
	// mail.ParseAddress accepts "John <john@example.com>" but we reject because
	// addr.Address != email (it's just the email part).
	if err := ValidateEmail("John <admin@example.com>"); err == nil {
		t.Fatalf("expected invalid email error for display-name format")
	}
}

func TestValidateDomainRejectsDomainStartingWithURLScheme(t *testing.T) {
	for _, domain := range []string{"https://example.com", "http://example.com", "ftp://example.com"} {
		t.Run(domain, func(t *testing.T) {
			if err := ValidateDomain(domain); err == nil {
				t.Fatalf("expected invalid domain error for %q", domain)
			}
		})
	}
}
