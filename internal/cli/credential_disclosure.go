package cli

import (
	"fmt"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

func installCredentialSummary(profile installer.RURecommendedProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Panel: %s\n", installPanelURL(profile))
	fmt.Fprintf(&b, "Username: %s\n", profile.Username)
	return b.String()
}

func installPanelURL(profile installer.RURecommendedProfile) string {
	if profile.Domain != "" && profile.WebBasePath != "" {
		return "https://" + profile.Domain + profile.WebBasePath
	}
	if profile.PanelListen != "" {
		scheme := "http"
		if profile.PanelTLSEnabled {
			scheme = "https"
		}
		return scheme + "://" + profile.PanelListen + "/"
	}
	return "https://" + profile.Domain + profile.WebBasePath
}
