package legacy

import "strings"

const panelStack = "panel"

func IsPanelOnlyStack(value string) bool {
	return value == "" || value == panelStack
}

func IsTrimmedPanelOnlyStack(value string) bool {
	return IsPanelOnlyStack(strings.TrimSpace(value))
}

func NormalizeSettingsStack(value string) (string, bool) {
	switch value {
	case "", panelStack, "both", "naive", "hysteria2", "mieru":
		return panelStack, true
	default:
		return "", false
	}
}
