package clientaccess

import "strings"

func protocolString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(s)
}

func protocolBool(m map[string]any, key string, fallback bool) bool {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

type ClientAccessProtocolRegistry struct {
	protocols map[string]ClientAccessProtocol
}

type ClientAccessProtocol struct {
	Protocol       string
	ProfileLink    func(ClientAccessLinkInput) (ClientLink, bool)
	FallbackLink   func(ClientAccessLinkInput) (ClientLink, bool)
	AggregateLinks func(Settings, []Inbound) ([]ClientLink, error)
}

type ClientAccessLinkInput struct {
	Settings   Settings
	Inbound    Inbound
	LinkName   string
	Credential ClientCredential
}

func NewClientAccessProtocolRegistry() ClientAccessProtocolRegistry {
	return ClientAccessProtocolRegistry{protocols: map[string]ClientAccessProtocol{
		"naiveproxy": {
			Protocol:     "naiveproxy",
			ProfileLink:  naiveProfileClientLink,
			FallbackLink: naiveFallbackClientLink,
		},
		"hysteria2": {
			Protocol:     "hysteria2",
			ProfileLink:  hysteria2ProfileClientLink,
			FallbackLink: hysteria2FallbackClientLink,
		},
		"mieru": {
			Protocol:     "mieru",
			ProfileLink:  mieruClientConfigLink,
			FallbackLink: mieruFallbackClientLink,
			AggregateLinks: func(settings Settings, inbounds []Inbound) ([]ClientLink, error) {
				return NewMieruClientAccessAggregator().Build(settings, inbounds)
			},
		},
		"olcrtc": {
			Protocol:     "olcrtc",
			ProfileLink:  olcrtcProfileClientLink,
			FallbackLink: olcrtcFallbackClientLink,
		},
	}}
}

func (r ClientAccessProtocolRegistry) BuildAllLinks(settings Settings, inbounds []Inbound) ([]ClientLink, error) {
	byProtocol := map[string][]Inbound{}
	order := []string{}
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if _, ok := r.protocols[inbound.Protocol]; !ok {
			continue
		}
		if _, ok := byProtocol[inbound.Protocol]; !ok {
			order = append(order, inbound.Protocol)
		}
		byProtocol[inbound.Protocol] = append(byProtocol[inbound.Protocol], inbound)
	}
	links := []ClientLink{}
	for _, protocolName := range order {
		protocol := r.protocols[protocolName]
		selected := byProtocol[protocolName]
		if protocol.AggregateLinks != nil {
			aggregated, err := protocol.AggregateLinks(settings, selected)
			if err != nil {
				return nil, err
			}
			links = append(links, aggregated...)
			continue
		}
		for _, inbound := range selected {
			access, err := BuildClientAccess(settings, inbound)
			if err != nil {
				return nil, err
			}
			links = append(links, r.BuildLinks(settings, inbound, access.credentials)...)
		}
	}
	return links, nil
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

func naiveProfileClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	if !hasClientEndpoint(input.Settings) {
		return ClientLink{}, false
	}
	link := newProtocolClientLink(input)
	link.URI = NaiveClientURI(clientEndpoint(input.Settings), input.Inbound.Port, input.Credential.Username, input.Credential.Password)
	return link, true
}

func naiveFallbackClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	if !hasClientEndpoint(input.Settings) {
		return ClientLink{}, false
	}
	link := newProtocolClientLink(input)
	password := input.Inbound.Password
	if password == "" {
		password = protocolString(input.Inbound.ProtocolFields, "naivePassword", input.Inbound.NaivePassword)
		if password == "" {
			password = protocolString(input.Settings.ProtocolFields, "naivePassword", input.Settings.NaivePassword)
		}
	}
	username := protocolString(input.Inbound.ProtocolFields, "naiveUsername", input.Inbound.NaiveUsername)
	if username == "" {
		username = protocolString(input.Settings.ProtocolFields, "naiveUsername", input.Settings.NaiveUsername)
	}
	link.URI = NaiveClientURI(clientEndpoint(input.Settings), input.Inbound.Port, username, password)
	return link, true
}

func hysteria2Insecure(input ClientAccessLinkInput) bool {
	if input.Inbound.Hysteria2Insecure {
		return true
	}
	if protocolBool(input.Inbound.ProtocolFields, "hysteria2Insecure", false) {
		return true
	}
	if input.Settings.Hysteria2Insecure {
		return true
	}
	return protocolBool(input.Settings.ProtocolFields, "hysteria2Insecure", false)
}

func hysteria2ProfileClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	if !hasClientEndpoint(input.Settings) {
		return ClientLink{}, false
	}
	link := newProtocolClientLink(input)
	link.URI = Hysteria2UserPassClientURI(clientEndpoint(input.Settings), input.Inbound.Port, input.Credential.Username, input.Credential.Password, link.Name, hysteria2Insecure(input))
	return link, true
}

func hysteria2FallbackClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	if !hasClientEndpoint(input.Settings) {
		return ClientLink{}, false
	}
	link := newProtocolClientLink(input)
	password := input.Inbound.Password
	if password == "" {
		password = protocolString(input.Inbound.ProtocolFields, "hysteria2Password", input.Inbound.Hysteria2Password)
		if password == "" {
			password = protocolString(input.Settings.ProtocolFields, "hysteria2Password", input.Settings.Hysteria2Password)
		}
	}
	link.URI = Hysteria2ClientURI(clientEndpoint(input.Settings), input.Inbound.Port, password, input.Inbound.Name, hysteria2Insecure(input))
	return link, true
}

func mieruClientConfigLink(input ClientAccessLinkInput) (ClientLink, bool) {
	if !hasClientEndpoint(input.Settings) {
		return ClientLink{}, false
	}
	link := newProtocolClientLink(input)
	config, err := NewMieruClientConfig().Build(input.Settings, input.Inbound, link.Name, input.Credential)
	if err != nil {
		return ClientLink{}, false
	}
	link.Config = config
	return link, true
}

func mieruFallbackClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	input.Credential = ClientCredential{Name: input.Inbound.Name, Username: input.Inbound.Name, Password: input.Inbound.Password}
	return mieruClientConfigLink(input)
}

func hasClientEndpoint(settings Settings) bool {
	return clientEndpoint(settings) != ""
}

func clientEndpoint(settings Settings) string {
	return strings.TrimSpace(settings.Domain)
}

func olcrtcProfileClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	link := newProtocolClientLink(input)
	auth := protocolString(input.Inbound.ProtocolFields, "olcrtcAuth", input.Inbound.OlcrtcAuth)
	if auth == "" {
		auth = protocolString(input.Settings.ProtocolFields, "olcrtcAuth", input.Settings.OlcrtcAuth)
	}
	transport := protocolString(input.Inbound.ProtocolFields, "olcrtcTransport", input.Inbound.OlcrtcTransport)
	if transport == "" {
		transport = protocolString(input.Settings.ProtocolFields, "olcrtcTransport", input.Settings.OlcrtcTransport)
	}
	roomID := protocolString(input.Inbound.ProtocolFields, "olcrtcRoomID", input.Inbound.OlcrtcRoomID)
	if roomID == "" {
		roomID = protocolString(input.Settings.ProtocolFields, "olcrtcRoomID", input.Settings.OlcrtcRoomID)
	}
	// olcRTC authenticates with a single shared crypto key (the inbound key),
	// not a per-profile secret, so every client link carries the inbound key.
	link.URI = OlcrtcClientURI(
		auth,
		transport,
		roomID,
		input.Inbound.Password,
		input.Credential.Username,
	)
	return link, true
}

func olcrtcFallbackClientLink(input ClientAccessLinkInput) (ClientLink, bool) {
	link := newProtocolClientLink(input)
	auth := protocolString(input.Inbound.ProtocolFields, "olcrtcAuth", input.Inbound.OlcrtcAuth)
	if auth == "" {
		auth = protocolString(input.Settings.ProtocolFields, "olcrtcAuth", input.Settings.OlcrtcAuth)
	}
	transport := protocolString(input.Inbound.ProtocolFields, "olcrtcTransport", input.Inbound.OlcrtcTransport)
	if transport == "" {
		transport = protocolString(input.Settings.ProtocolFields, "olcrtcTransport", input.Settings.OlcrtcTransport)
	}
	roomID := protocolString(input.Inbound.ProtocolFields, "olcrtcRoomID", input.Inbound.OlcrtcRoomID)
	if roomID == "" {
		roomID = protocolString(input.Settings.ProtocolFields, "olcrtcRoomID", input.Settings.OlcrtcRoomID)
	}
	link.URI = OlcrtcClientURI(
		auth,
		transport,
		roomID,
		input.Inbound.Password,
		"",
	)
	return link, true
}
