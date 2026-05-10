package api

import (
	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/panelaccess"
	"github.com/veil-panel/veil/internal/protocols"
)

type PanelAccess struct {
	inner panelaccess.PanelAccess
}

type PanelAccessApplyIntent = panelaccess.ApplyIntent

func NewPanelAccess(settings Settings) PanelAccess {
	return PanelAccess{inner: panelaccess.New(settings, protocols.NewCatalog().RequiresCaddy)}
}

func (p PanelAccess) CaddyRoute() (panelaccess.CaddyRoute, bool, error) {
	return p.inner.CaddyRoute()
}

func (p PanelAccess) GeneratedConfig(paths generatedconfig.Paths) (generatedconfig.GeneratedConfigArtifact, bool, error) {
	return p.inner.GeneratedConfig(paths)
}

func (p PanelAccess) ApplyIntent(inbounds []Inbound) PanelAccessApplyIntent {
	return p.inner.ApplyIntent(inbounds)
}
