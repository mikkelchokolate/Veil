package api

func normalizedSettingsStack(stack string) string {
	normalized, ok := NormalizeSettingsStack(stack)
	if !ok {
		return "panel"
	}
	return normalized
}

func NormalizeSettingsStack(stack string) (string, bool) {
	switch stack {
	case "", "panel", "both", "naive", "hysteria2", "mieru":
		return "panel", true
	default:
		return "", false
	}
}
