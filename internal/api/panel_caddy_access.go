package api

import "github.com/veil-panel/veil/internal/panelaccess"

type PanelCaddyAccess struct{}

type PanelCaddyRoute = panelaccess.CaddyRoute

func NewPanelCaddyAccess() PanelCaddyAccess { return PanelCaddyAccess{} }

func (PanelCaddyAccess) Route(settings Settings) (PanelCaddyRoute, bool, error) {
	return NewPanelAccess(settings).CaddyRoute()
}
