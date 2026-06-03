package clientaccess

import "github.com/mikkelchokolate/Veil/internal/renderer"

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
	return NewClientAccessProtocolRegistry().BuildLinks(a.settings, a.inbound, a.credentials)
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
