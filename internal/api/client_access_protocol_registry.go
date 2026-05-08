package api

type ClientAccessProtocolRegistry struct {
	protocols map[string]ClientAccessProtocol
}

type ClientAccessProtocol struct {
	Protocol     string
	ProfileLink  func(ClientAccessLinkInput) (ClientLink, bool)
	FallbackLink func(ClientAccessLinkInput) (ClientLink, bool)
}

type ClientAccessLinkInput struct {
	Settings   Settings
	Inbound    Inbound
	LinkName   string
	Credential ClientCredential
}

func NewClientAccessProtocolRegistry() ClientAccessProtocolRegistry {
	protocols := []ClientAccessProtocol{
		{
			Protocol: "naiveproxy",
			ProfileLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				link := newProtocolClientLink(input)
				link.URI = naiveClientURI(input.Settings.Domain, input.Inbound.Port, input.Credential.Username, input.Credential.Password)
				return link, true
			},
			FallbackLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				link := newProtocolClientLink(input)
				password := input.Inbound.Password
				if password == "" {
					password = input.Settings.NaivePassword
				}
				link.URI = naiveClientURI(input.Settings.Domain, input.Inbound.Port, input.Settings.NaiveUsername, password)
				return link, true
			},
		},
		{
			Protocol: "hysteria2",
			ProfileLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				link := newProtocolClientLink(input)
				link.URI = hysteria2UserPassClientURI(input.Settings.Domain, input.Inbound.Port, input.Credential.Username, input.Credential.Password, link.Name)
				return link, true
			},
			FallbackLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				link := newProtocolClientLink(input)
				password := input.Inbound.Password
				if password == "" {
					password = input.Settings.Hysteria2Password
				}
				link.URI = hysteria2ClientURI(input.Settings.Domain, input.Inbound.Port, password, input.Inbound.Name)
				return link, true
			},
		},
		{
			Protocol: "mieru",
			ProfileLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				return mieruClientConfigLink(input)
			},
			FallbackLink: func(input ClientAccessLinkInput) (ClientLink, bool) {
				input.Credential = ClientCredential{Name: input.Inbound.Name, Username: input.Inbound.Name, Password: input.Inbound.Password}
				return mieruClientConfigLink(input)
			},
		},
	}
	byProtocol := map[string]ClientAccessProtocol{}
	for _, protocol := range protocols {
		byProtocol[protocol.Protocol] = protocol
	}
	return ClientAccessProtocolRegistry{protocols: byProtocol}
}

func (r ClientAccessProtocolRegistry) BuildLinks(settings Settings, inbound Inbound, credentials []ClientCredential) []ClientLink {
	protocol, ok := r.protocols[inbound.Protocol]
	if !ok {
		return nil
	}
	if len(credentials) == 0 {
		link, ok := protocol.FallbackLink(ClientAccessLinkInput{Settings: settings, Inbound: inbound, LinkName: inbound.Name})
		if !ok {
			return nil
		}
		return []ClientLink{link}
	}
	links := make([]ClientLink, 0, len(credentials))
	for _, credential := range credentials {
		linkName := inbound.Name + "/" + credential.Name
		link, ok := protocol.ProfileLink(ClientAccessLinkInput{Settings: settings, Inbound: inbound, LinkName: linkName, Credential: credential})
		if ok {
			links = append(links, link)
		}
	}
	return links
}

func newProtocolClientLink(input ClientAccessLinkInput) ClientLink {
	return ClientLink{Name: input.LinkName, Protocol: input.Inbound.Protocol, Transport: input.Inbound.Transport, Port: input.Inbound.Port}
}

func mieruClientConfigLink(input ClientAccessLinkInput) (ClientLink, bool) {
	link := newProtocolClientLink(input)
	config, err := NewMieruClientConfig().Build(input.Settings, input.Inbound, link.Name, input.Credential)
	if err != nil {
		return ClientLink{}, false
	}
	link.Config = config
	return link, true
}
