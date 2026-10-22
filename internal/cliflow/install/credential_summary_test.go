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

func TestDirectPanelURLIncludesListenerPort(t *testing.T) {
	for _, test := range []struct {
		name, domain, listen, want string
	}{
		{"IPv4", "203.0.113.4", "0.0.0.0:2096", "https://203.0.113.4:2096/secret/"},
		{"IPv6", "2001:db8::4", "[::]:2096", "https://[2001:db8::4]:2096/secret/"},
		{"domain and random port", "panel.example.com", "0.0.0.0:35501", "https://panel.example.com:35501/secret/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := installer.RURecommendedProfile{
				PanelAccess: "direct", PanelListen: test.listen, Domain: test.domain,
				PanelTLSEnabled: true, WebBasePath: "/secret/",
			}
			if got := PanelURL(profile); got != test.want {
				t.Fatalf("PanelURL = %q, want %q", got, test.want)
			}
			for _, summary := range []string{
				CredentialSummary(profile), NewPanelSummary(PanelSummaryInput{Profile: profile}).String(),
			} {
				if !strings.Contains(summary, test.want) {
					t.Fatalf("summary missing direct URL %q:\n%s", test.want, summary)
				}
			}
		})
	}
}
