package api

import "errors"

func ValidateSettingsStackCompatibility(settings Settings) error {
	_, ok := normalizeLegacySettingsStack(settings.legacyStack)
	if !ok {
		return errors.New("stack must be panel; protocols are configured as Panel inbounds")
	}
	return nil
}

func LegacySettingsStack(settings Settings) string {
	return settings.legacyStack
}

func normalizeLegacySettingsStack(stack string) (string, bool) {
	switch stack {
	case "", "panel", "both", "naive", "hysteria2", "mieru":
		return "panel", true
	default:
		return "", false
	}
}
