package cli

import (
	"fmt"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

func installCredentialSummary(profile installer.RURecommendedProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Panel: https://%s%s\n", profile.Domain, profile.WebBasePath)
	fmt.Fprintf(&b, "Username: %s\n", profile.Username)
	if profile.InstallNaive && profile.NaivePassword != "" {
		fmt.Fprintf(&b, "NaiveProxy password: %s\n", profile.NaivePassword)
	}
	if profile.InstallHysteria2 && profile.Hysteria2Password != "" {
		fmt.Fprintf(&b, "Hysteria2 password: %s\n", profile.Hysteria2Password)
	}
	return b.String()
}
