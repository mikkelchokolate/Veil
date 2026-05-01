package cli

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestRedactProfileSecretsHidesGeneratedCredentials(t *testing.T) {
	profile := installer.RURecommendedProfile{
		NaivePassword:      "naive-secret",
		Hysteria2Password:  "hy2-secret",
		PanelAuthToken:     "panel-secret",
		NaiveClientURL:     "https://veil:naive-secret@example.com:443",
		Hysteria2ClientURI: "hysteria2://hy2-secret@example.com:443?insecure=0",
		Caddyfile:          "basicauth veil naive-secret",
		Hysteria2YAML:      "password: hy2-secret",
	}
	input := strings.Join([]string{
		profile.NaiveClientURL,
		profile.Hysteria2ClientURI,
		profile.Caddyfile,
		profile.Hysteria2YAML,
		profile.PanelAuthToken,
	}, "\n")

	got := redactProfileSecrets(profile, input)

	for _, secret := range []string{"naive-secret", "hy2-secret", "panel-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted output still contains %q:\n%s", secret, got)
		}
	}
	if strings.Count(got, "[REDACTED]") < 3 {
		t.Fatalf("expected redaction markers, got:\n%s", got)
	}
}
