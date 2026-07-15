package panel

import "strings"

// panelInboundReliableActionsJS removes the legacy mount-time request before
// adding the reliability and control layers. The tab loader and explicit
// refresh control are the only entry points that should fetch the inbound list;
// otherwise a hidden tab can issue a redundant request and repaint stale UI.
func panelInboundReliableActionsJS() string {
	actions := panelInboundActionsJS()
	actions = strings.Replace(actions, `      setTimeout(loadInboundsIntoOutput, 500);`, "", 1)
	return actions + panelInboundReliabilityJS() + panelInboundControlsJS()
}
