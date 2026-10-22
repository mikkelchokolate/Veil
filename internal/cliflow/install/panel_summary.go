package install

import (
	"fmt"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/installer"
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
	if profile.PanelListen == "" {
		profile.PanelListen = fmt.Sprintf("127.0.0.1:%d", input.PanelPort)
	}
	if profile.Domain != "" {
		fmt.Fprintf(&b, "Panel URL: %s\n", PanelURL(profile))
		return b.String()
	}
	fmt.Fprintf(&b, "Panel access: %s\n", PanelURL(profile))
	return b.String()
}
