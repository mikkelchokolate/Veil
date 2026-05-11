package install

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestInstallPresentationPrintsRedactedRURecommendedProfile(t *testing.T) {
	profile := installer.RURecommendedProfile{
		Domain:            "example.com",
		Email:             "admin@example.com",
		InstallPanelCaddy: true,
		Caddyfile:         "reverse_proxy 127.0.0.1:2096 # panel-secret",
		PanelAuthToken:    "panel-secret",
	}
	var out bytes.Buffer

	NewPresentation(&out).PrintRURecommended(profile, true)

	got := out.String()
	for _, want := range []string{"Veil ru-recommended dry run", "Domain: example.com", "Install scope: Panel", "[REDACTED]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Stack:") {
		t.Fatalf("install output should not expose protocol stack language:\n%s", got)
	}
	for _, secret := range []string{"panel-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("output leaked %q:\n%s", secret, got)
		}
	}
}
