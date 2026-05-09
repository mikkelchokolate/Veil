package api

import (
	"errors"

	"github.com/veil-panel/veil/internal/legacy"
)

func ValidateSettingsStackCompatibility(settings Settings) error {
	if !legacy.AcceptsSettingsStackJSON(settings.legacyStack) {
		return errors.New("stack must be panel; protocols are configured as Panel inbounds")
	}
	return nil
}

func LegacySettingsStack(settings Settings) string {
	return settings.legacyStack
}
