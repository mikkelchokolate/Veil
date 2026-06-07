package install

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestCredentialSummaryDisplaysHTTPSPanelAccessWithoutCaddy(t *testing.T) {
	summary := CredentialSummary(installer.RURecommendedProfile{PanelListen: "127.0.0.1:2096", PanelTLSEnabled: true, Username: "veil"})
	for _, want := range []string{"Panel: https://127.0.0.1:2096/", "Username: veil"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestCredentialSummaryIncludesWebBasePathForListenAccess(t *testing.T) {
	// Local/direct access has no domain; the secret web base path must still be
	// part of the printed URL, otherwise the address returns 404.
	summary := CredentialSummary(installer.RURecommendedProfile{
		PanelListen:     "127.0.0.1:2096",
		PanelTLSEnabled: true,
		WebBasePath:     "/U1skQ-q51_m3/",
		Username:        "admin_f76e",
	})
	want := "Panel: https://127.0.0.1:2096/U1skQ-q51_m3/"
	if !strings.Contains(summary, want) {
		t.Fatalf("summary missing %q:\n%s", want, summary)
	}
}

func TestCredentialSummaryDisplaysPanelOnlyCredentials(t *testing.T) {
	summary := CredentialSummary(installer.RURecommendedProfile{
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
