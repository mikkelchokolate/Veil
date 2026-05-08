package api

func NormalizeSettingsStack(stack string) (string, bool) {
	switch stack {
	case "", "panel", "both", "naive", "hysteria2", "mieru":
		return "panel", true
	default:
		return "", false
	}
}
