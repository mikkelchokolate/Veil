package client

import (
	"errors"
	"regexp"
	"strings"
)

var runtimeIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

func GenerateRuntimeIdentity(bindingID string) string {
	compact := strings.ToLower(strings.ReplaceAll(bindingID, "-", ""))
	if len(compact) > 32 {
		compact = compact[:32]
	}
	return "v_" + compact
}

func ValidateRuntimeIdentity(value string) error {
	if !runtimeIdentityPattern.MatchString(value) {
		return errors.New("runtime identity must be 1-48 ASCII letters, digits, '_' or '-'")
	}
	return nil
}
