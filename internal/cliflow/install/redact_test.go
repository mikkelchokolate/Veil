package install

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestRedactProfileSecretsHidesPanelCredential(t *testing.T) {
	profile := installer.RURecommendedProfile{PanelAuthToken: "panel-secret"}
	input := "Panel token: panel-secret"

	got := NewPresentation(nil).RedactProfileSecrets(profile, input)

	if strings.Contains(got, "panel-secret") {
		t.Fatalf("redacted output still contains panel token:\n%s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got:\n%s", got)
	}
}
