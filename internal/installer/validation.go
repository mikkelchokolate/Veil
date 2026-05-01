package installer

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

var domainLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	if strings.Contains(domain, "://") || strings.ContainsAny(domain, " /_") {
		return fmt.Errorf("domain must be a hostname, not a URL")
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain is too long")
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must include at least one dot")
	}
	for _, label := range labels {
		if !domainLabelPattern.MatchString(label) {
			return fmt.Errorf("invalid domain label %q", label)
		}
	}
	return nil
}

func ValidateEmail(email string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("invalid email: %w", err)
	}
	if addr.Address != email || !strings.Contains(addr.Address, "@") {
		return fmt.Errorf("invalid email")
	}
	return nil
}
