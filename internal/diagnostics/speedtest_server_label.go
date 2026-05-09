package diagnostics

import "strings"

type SpeedtestServerLabel struct {
	Provider string
	Server   string
}

func NewSpeedtestServerLabel(provider, server string) SpeedtestServerLabel {
	return SpeedtestServerLabel{Provider: provider, Server: server}
}

func (l SpeedtestServerLabel) String() string {
	provider := strings.TrimSpace(l.Provider)
	server := strings.TrimSpace(l.Server)
	if provider != "" && server != "" {
		return provider + " - " + server
	}
	if provider != "" {
		return provider
	}
	return server
}
