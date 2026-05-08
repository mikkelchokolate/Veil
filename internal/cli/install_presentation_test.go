package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestInstallPresentationPrintsRedactedRURecommendedProfile(t *testing.T) {
	profile := installer.RURecommendedProfile{
		Domain:         "example.com",
		Email:          "admin@example.com",
		InstallNaive:   true,
		NaivePassword:  "naive-secret",
		NaiveClientURL: "naive+https://veil:naive-secret@example.com:443",
		Caddyfile:      "basicauth veil naive-secret",
		PanelAuthToken: "panel-secret",
		PortPlan:       installer.SharedPortPlan{Naive: installer.EndpointPlan{Port: 443}},
	}
	var out bytes.Buffer

	NewInstallPresentation(&out).PrintRURecommended(profile, true)

	got := out.String()
	for _, want := range []string{"Veil ru-recommended dry run", "Domain: example.com", "Stack: naive", "[REDACTED]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{"naive-secret", "panel-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("output leaked %q:\n%s", secret, got)
		}
	}
}
