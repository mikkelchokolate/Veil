package installer

import (
	"strings"
	"testing"
)

func TestInstallPlanSummaryDoesNotPrintSharedPortWithoutSharedProxyRuntime(t *testing.T) {
	plan := InstallPlan{Profile: RURecommendedProfile{Stack: Stack("mieru"), InstallMieru: true}, MieruBinary: BinaryAcquisition{URL: "https://example.com/mieru", Destination: "/usr/local/bin/mieru"}}
	summary := NewInstallPlanSummary(plan).String()
	if strings.Contains(summary, "Shared port:") || strings.Contains(summary, "NaiveProxy:") || strings.Contains(summary, "Hysteria2:") {
		t.Fatalf("Mieru runtime summary should not mention shared proxy port:\n%s", summary)
	}
	if !strings.Contains(summary, "Mieru asset: https://example.com/mieru") {
		t.Fatalf("summary missing Mieru runtime details:\n%s", summary)
	}
}

func TestInstallPlanSummaryIncludesCoreInstallArtifacts(t *testing.T) {
	plan := InstallPlan{
		Profile:        RURecommendedProfile{InstallHysteria2: true},
		HysteriaURL:    "https://example.com/hysteria",
		HysteriaBinary: BinaryAcquisition{Destination: "/usr/local/bin/hysteria"},
		PanelTools:     []string{"speedtest-cli or speedtest"},
	}
	summary := NewInstallPlanSummary(plan).String()
	for _, want := range []string{"Hysteria2 asset: https://example.com/hysteria", "Panel speedtest tool: speedtest-cli or speedtest"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"Shared port:", "NaiveProxy:", "Hysteria2: udp/"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("summary should not include %q:\n%s", unwanted, summary)
		}
	}
}
