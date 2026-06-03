package install

import (
	"fmt"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

func CredentialSummary(profile installer.RURecommendedProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Panel: %s\n", PanelURL(profile))
	fmt.Fprintf(&b, "Username: %s\n", profile.Username)
	if profile.Password != "" {
		fmt.Fprintf(&b, "Password: %s\n", profile.Password)
	} else {
		fmt.Fprintf(&b, "Password: [preserved existing password]\n")
	}
	return b.String()
}

func PanelURL(profile installer.RURecommendedProfile) string {
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
