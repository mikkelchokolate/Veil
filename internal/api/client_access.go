package api

import "github.com/veil-panel/veil/internal/renderer"

type ClientAccess struct {
	settings    Settings
	inbound     Inbound
	credentials []ClientCredential
}

func BuildClientAccess(settings Settings, inbound Inbound) (ClientAccess, error) {
	credentials, err := BuildClientCredentials(inbound)
	if err != nil {
		return ClientAccess{}, err
	}
	return ClientAccess{settings: settings, inbound: inbound, credentials: credentials}, nil
}

func (a ClientAccess) ClientLinks() []ClientLink {
	credentials := a.credentials
	fallback := len(credentials) == 0
	if fallback && a.inbound.Protocol != "mieru" {
		return []ClientLink{fallbackInboundClientLink(a.settings, a.inbound)}
	}
	if fallback {
		credentials = []ClientCredential{NewClientAccessFallbackCredential().Build(a.settings, a.inbound)}
	}
	links := make([]ClientLink, 0, len(credentials))
	for _, credential := range credentials {
		name := a.inbound.Name + "/" + credential.Name
		if fallback {
			name = a.inbound.Name
		}
		link := ClientLink{Name: name, Protocol: a.inbound.Protocol, Transport: a.inbound.Transport, Port: a.inbound.Port}
		switch a.inbound.Protocol {
		case "naiveproxy":
			link.URI = naiveClientURI(a.settings.Domain, a.inbound.Port, credential.Username, credential.Password)
		case "hysteria2":
			link.URI = hysteria2UserPassClientURI(a.settings.Domain, a.inbound.Port, credential.Username, credential.Password, link.Name)
		case "mieru":
			config, err := NewMieruClientConfig().Build(a.settings, a.inbound, link.Name, credential)
			if err != nil {
				continue
			}
			link.Config = config
		default:
			continue
		}
		links = append(links, link)
	}
	return links
}

func (a ClientAccess) NaiveUsers() []renderer.NaiveUser {
	users := make([]renderer.NaiveUser, 0, len(a.credentials))
	for _, credential := range a.credentials {
		users = append(users, renderer.NaiveUser{Username: credential.Username, Password: credential.Password})
	}
	return users
}

func (a ClientAccess) Hysteria2Users() []renderer.Hysteria2User {
	users := make([]renderer.Hysteria2User, 0, len(a.credentials))
	for _, credential := range a.credentials {
		users = append(users, renderer.Hysteria2User{Username: credential.Username, Password: credential.Password})
	}
	return users
}
