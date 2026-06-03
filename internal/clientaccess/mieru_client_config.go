package clientaccess

import "github.com/mikkelchokolate/Veil/internal/renderer"

type MieruClientConfig struct{}

func NewMieruClientConfig() MieruClientConfig { return MieruClientConfig{} }

func (c MieruClientConfig) Build(settings Settings, inbound Inbound, linkName string, credential ClientCredential) (string, error) {
	return c.BuildWithBindings(settings, []Inbound{inbound}, linkName, credential)
}

func (MieruClientConfig) BuildWithBindings(settings Settings, inbounds []Inbound, linkName string, credential ClientCredential) (string, error) {
	bindings := make([]renderer.MieruPortBinding, 0, len(inbounds))
	for _, inbound := range inbounds {
		bindings = append(bindings, renderer.MieruPortBinding{Port: inbound.Port, Protocol: inbound.Transport})
	}
	return renderer.RenderMieruClient(renderer.MieruClientConfig{
		ProfileName:  linkName,
		DomainName:   settings.Domain,
		PortBindings: bindings,
		User:         renderer.MieruUser{Name: credential.Username, Password: credential.Password},
	})
}
