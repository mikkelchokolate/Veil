package cli

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestInstallCredentialSummaryDisplaysHTTPSPanelAccessWithoutCaddy(t *testing.T) {
	summary := installCredentialSummary(installer.RURecommendedProfile{PanelListen: "127.0.0.1:2096", PanelTLSEnabled: true, Username: "veil"})
	for _, want := range []string{"Panel: https://127.0.0.1:2096/", "Username: veil"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestInstallCredentialSummaryDisplaysPanelOnlyCredentials(t *testing.T) {
	summary := installCredentialSummary(installer.RURecommendedProfile{
		Domain:      "vpn.example.com",
		WebBasePath: "/secret/",
		Username:    "veil",
	})
	for _, want := range []string{
		"Panel: https://vpn.example.com/secret/",
		"Username: veil",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"NaiveProxy password:", "Hysteria2 password:"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("summary should not include protocol credential %q:\n%s", unwanted, summary)
		}
	}
}
