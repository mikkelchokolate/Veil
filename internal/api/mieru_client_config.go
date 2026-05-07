package api

import "github.com/veil-panel/veil/internal/renderer"

type MieruClientConfig struct{}

func NewMieruClientConfig() MieruClientConfig { return MieruClientConfig{} }

func (MieruClientConfig) Build(settings Settings, inbound Inbound, linkName string, credential ClientCredential) (string, error) {
	return renderer.RenderMieruClient(renderer.MieruClientConfig{
		ProfileName:  linkName,
		DomainName:   settings.Domain,
		PortBindings: []renderer.MieruPortBinding{{Port: inbound.Port, Protocol: inbound.Transport}},
		User:         renderer.MieruUser{Name: credential.Username, Password: credential.Password},
	})
}
