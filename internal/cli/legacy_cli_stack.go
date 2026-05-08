package cli

import (
	"fmt"
	"strings"
)

func rejectLegacyCLIStackSelection(stack string, message string) error {
	stack = strings.TrimSpace(stack)
	if stack == "" || stack == "panel" {
		return nil
	}
	return fmt.Errorf(message)
}
