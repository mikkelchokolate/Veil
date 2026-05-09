package install

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestPanelSummaryRendersCaddyPanelURLAndPortSource(t *testing.T) {
	summary := NewPanelSummary(PanelSummaryInput{Profile: installer.RURecommendedProfile{Domain: "panel.example.com", WebBasePath: "/secret/"}, PanelPort: 2096, PanelPortSet: true})
	text := summary.String()
	for _, want := range []string{"Panel port: 2096 (user selected)", "Panel URL: https://panel.example.com/secret/"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}

func TestPanelSummaryRendersLocalTLSAccess(t *testing.T) {
	summary := NewPanelSummary(PanelSummaryInput{Profile: installer.RURecommendedProfile{PanelListen: "127.0.0.1:2096", PanelTLSEnabled: true}, PanelPort: 2096})
	text := summary.String()
	for _, want := range []string{"Panel port: 2096 (default)", "Panel access: https://127.0.0.1:2096/"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}
