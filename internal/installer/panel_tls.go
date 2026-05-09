package installer

import "github.com/veil-panel/veil/internal/panelaccess"

type PanelTLS = panelaccess.TLS
type PanelTLSMaterial = panelaccess.TLSMaterial

func NewPanelTLS() PanelTLS { return panelaccess.NewTLS() }
