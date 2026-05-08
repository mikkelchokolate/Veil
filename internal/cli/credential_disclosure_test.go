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

func TestInstallCredentialSummaryDisplaysPanelAndProxyPasswords(t *testing.T) {
	summary := installCredentialSummary(installer.RURecommendedProfile{
		Domain:            "vpn.example.com",
		WebBasePath:       "/secret/",
		Username:          "veil",
		InstallNaive:      true,
		InstallHysteria2:  true,
		NaivePassword:     "naive-pass",
		Hysteria2Password: "hy2-pass",
	})
	for _, want := range []string{
		"Panel: https://vpn.example.com/secret/",
		"Username: veil",
		"NaiveProxy password: naive-pass",
		"Hysteria2 password: hy2-pass",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}
