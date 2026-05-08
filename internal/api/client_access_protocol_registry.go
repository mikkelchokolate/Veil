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
	byProtocol := map[string]ClientAccessProtocol{}
	for _, capability := range NewProtocolCapabilityCatalog().All() {
		if capability.ProfileClientLink == nil && capability.FallbackClientLink == nil {
			continue
		}
		byProtocol[capability.Protocol] = ClientAccessProtocol{Protocol: capability.Protocol, ProfileLink: capability.ProfileClientLink, FallbackLink: capability.FallbackClientLink}
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
