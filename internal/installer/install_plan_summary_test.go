package installer

import (
	"strings"
	"testing"
)

func TestInstallPlanSummaryDoesNotPrintLegacyProtocolRuntimeArtifacts(t *testing.T) {
	plan := InstallPlan{PanelTools: []string{"speedtest-cli or speedtest"}}
	summary := plan.Summary()
	for _, unwanted := range []string{"Shared port:", "NaiveProxy:", "Hysteria2 asset:", "Mieru asset:"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("summary should not include legacy protocol install artifact %q:\n%s", unwanted, summary)
		}
	}
	if !strings.Contains(summary, "Panel speedtest tool: speedtest-cli or speedtest") {
		t.Fatalf("summary missing Panel tool:\n%s", summary)
	}
}

func TestInstallPlanSummaryIncludesPanelCaddyPrerequisite(t *testing.T) {
	plan := InstallPlan{
		Profile:    RURecommendedProfile{InstallPanelCaddy: true},
		CaddyBuild: CaddyPanelBuildHint("/usr/local/bin/caddy"),
	}
	summary := plan.Summary()
	if !strings.Contains(summary, "Caddy/Panel reverse proxy: /usr/local/bin/caddy") {
		t.Fatalf("summary missing Panel Caddy prerequisite:\n%s", summary)
	}
	if strings.Contains(summary, "Caddy/NaiveProxy build") {
		t.Fatalf("summary should not describe Panel Caddy as NaiveProxy:\n%s", summary)
	}
}
