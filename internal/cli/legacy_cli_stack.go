package cli

import (
	"fmt"
	"strings"
)

func rejectLegacyCLIStackSelection(legacyStack string, message string) error {
	legacyStack = strings.TrimSpace(legacyStack)
	if legacyStack == "" || legacyStack == "panel" {
		return nil
	}
	return fmt.Errorf(message)
}
