package install

import (
	"fmt"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

type PanelSummaryInput struct {
	Profile      installer.RURecommendedProfile
	PanelPort    int
	PanelRandom  bool
	PanelPortSet bool
}

type PanelSummary struct {
	input PanelSummaryInput
}

func NewPanelSummary(input PanelSummaryInput) PanelSummary {
	return PanelSummary{input: input}
}

func (s PanelSummary) String() string {
	input := s.input
	var b strings.Builder
	if input.PanelRandom {
		fmt.Fprintf(&b, "Panel port: %d (random)\n", input.PanelPort)
	} else if input.PanelPortSet {
		fmt.Fprintf(&b, "Panel port: %d (user selected)\n", input.PanelPort)
	} else {
		fmt.Fprintf(&b, "Panel port: %d (default)\n", input.PanelPort)
	}
	profile := input.Profile
	if profile.WebBasePath != "" && profile.WebBasePath != "/" {
		fmt.Fprintf(&b, "Panel URL: https://%s%s\n", profile.Domain, profile.WebBasePath)
		return b.String()
	}
	scheme := "http"
	if profile.PanelTLSEnabled {
		scheme = "https"
	}
	panelListen := profile.PanelListen
	if panelListen == "" {
		panelListen = fmt.Sprintf("127.0.0.1:%d", input.PanelPort)
	}
	fmt.Fprintf(&b, "Panel access: %s://%s/\n", scheme, panelListen)
	return b.String()
}
