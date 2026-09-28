package client

import (
	"errors"
	"regexp"
	"strings"
)

// Runtime identities are canonical lowercase. Hysteria2 lowercases the
// username it reports for accounting, so a stored mixed-case identity would
// never match its own traffic counters, and two identities differing only by
// case collapse into the same runtime user (#1111). Storage additionally
// enforces inbound-uniqueness with a NOCASE index (migration 29).
var runtimeIdentityPattern = regexp.MustCompile(`^[a-z0-9_-]{1,48}$`)

func GenerateRuntimeIdentity(bindingID string) string {
	compact := strings.ToLower(strings.ReplaceAll(bindingID, "-", ""))
	if len(compact) > 32 {
		compact = compact[:32]
	}
	return "v_" + compact
}

func ValidateRuntimeIdentity(value string) error {
	if !runtimeIdentityPattern.MatchString(value) {
		return errors.New("runtime identity must be 1-48 lowercase ASCII letters, digits, '_' or '-'")
	}
	return nil
}
