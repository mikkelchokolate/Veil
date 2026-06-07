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
	// The web base path is a secret prefix the panel is actually served under;
	// it must be part of the URL or the printed address returns 404.
	basePath := profile.WebBasePath
	if basePath == "" {
		basePath = "/"
	}
	if profile.Domain != "" {
		return "https://" + profile.Domain + basePath
	}
	if profile.PanelListen != "" {
		scheme := "http"
		if profile.PanelTLSEnabled {
			scheme = "https"
		}
		return scheme + "://" + profile.PanelListen + basePath
	}
	return "https://" + profile.Domain + basePath
}
