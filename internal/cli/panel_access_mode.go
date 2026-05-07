package cli

import "fmt"

type PanelAccessResolution struct {
	Mode          string
	PanelListen   string
	RequiresCaddy bool
}

type PanelAccessMode struct {
	mode string
}

func NewPanelAccessMode(mode string) PanelAccessMode {
	return PanelAccessMode{mode: mode}
}

func (m PanelAccessMode) Resolve(port int) (PanelAccessResolution, error) {
	mode := m.mode
	if mode == "" {
		mode = "local"
	}
	switch mode {
	case "direct":
		return PanelAccessResolution{Mode: mode, PanelListen: fmt.Sprintf("0.0.0.0:%d", port)}, nil
	case "local":
		return PanelAccessResolution{Mode: mode, PanelListen: fmt.Sprintf("127.0.0.1:%d", port)}, nil
	case "caddy":
		return PanelAccessResolution{Mode: mode, PanelListen: fmt.Sprintf("127.0.0.1:%d", port), RequiresCaddy: true}, nil
	default:
		return PanelAccessResolution{}, fmt.Errorf("panel access must be direct, local, or caddy")
	}
}
