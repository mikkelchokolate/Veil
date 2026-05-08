package api

import (
	"fmt"
	"net"
	"strconv"
)

type PanelCaddyAccess struct{}

type PanelCaddyRoute struct {
	Port        int
	WebBasePath string
}

func NewPanelCaddyAccess() PanelCaddyAccess { return PanelCaddyAccess{} }

func (PanelCaddyAccess) Route(settings Settings) (PanelCaddyRoute, bool, error) {
	if settings.PanelAccess != "caddy" {
		return PanelCaddyRoute{}, false, nil
	}
	webBasePath := normalizeSettingsWebBasePath(settings.WebBasePath)
	if webBasePath == "" {
		return PanelCaddyRoute{}, false, fmt.Errorf("webBasePath is required for caddy Panel access")
	}
	_, portText, err := net.SplitHostPort(settings.PanelListen)
	if err != nil {
		return PanelCaddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return PanelCaddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	return PanelCaddyRoute{Port: port, WebBasePath: webBasePath}, true, nil
}
