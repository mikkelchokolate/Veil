package api

type PanelCaddyAccess struct{}

type PanelCaddyRoute struct {
	Port        int
	WebBasePath string
}

func NewPanelCaddyAccess() PanelCaddyAccess { return PanelCaddyAccess{} }

func (PanelCaddyAccess) Route(settings Settings) (PanelCaddyRoute, bool, error) {
	return NewPanelAccess(settings).CaddyRoute()
}
