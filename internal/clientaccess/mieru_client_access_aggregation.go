package clientaccess

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type MieruClientAccessAggregator struct{}

type mieruClientAccessGroup struct {
	name       string
	credential ClientCredential
	inbounds   []Inbound
}

func NewMieruClientAccessAggregator() MieruClientAccessAggregator {
	return MieruClientAccessAggregator{}
}

func (MieruClientAccessAggregator) Build(settings Settings, inbounds []Inbound) ([]ClientLink, error) {
	if clientEndpoint(settings) == "" {
		return nil, nil
	}
	groups := map[string]*mieruClientAccessGroup{}
	order := []string{}
	for _, inbound := range inbounds {
		if !inbound.Enabled || inbound.Protocol != "mieru" {
			continue
		}
		credentials, err := BuildClientCredentials(inbound)
		if err != nil {
			return nil, err
		}
		if len(credentials) == 0 {
			password := model.EffectiveInboundPassword(inbound)
			if len(inbound.Profiles) > 0 || password == "" {
				continue
			}
			credential := ClientCredential{Name: inbound.Name, Username: inbound.Name, Password: password}
			addMieruClientAccessGroup(groups, &order, inbound.Name, credential, inbound)
			continue
		}
		for _, credential := range credentials {
			addMieruClientAccessGroup(groups, &order, mieruClientAccessProfileLinkName(credential), credential, inbound)
		}
	}
	links := make([]ClientLink, 0, len(order))
	for _, key := range order {
		group := groups[key]
		if len(group.inbounds) == 0 {
			continue
		}
		link, ok := BuildMieruAggregatedLink(settings, group.inbounds, group.name, group.credential)
		if !ok {
			continue
		}
		links = append(links, link)
	}
	return links, nil
}

// BuildMieruAggregatedLink emits one Mieru client config/URI whose portBindings
// cover every supplied inbound. Per-client export uses this so TCP+UDP bindings
// become a single profile instead of N single-port configs.
func BuildMieruAggregatedLink(settings Settings, inbounds []Inbound, linkName string, credential ClientCredential) (ClientLink, bool) {
	if clientEndpoint(settings) == "" || len(inbounds) == 0 {
		return ClientLink{}, false
	}
	config, err := NewMieruClientConfig().BuildWithBindings(settings, inbounds, linkName, credential)
	if err != nil {
		return ClientLink{}, false
	}
	first := inbounds[0]
	bindings := make([]MieruURIBinding, 0, len(inbounds))
	for _, inbound := range inbounds {
		bindings = append(bindings, MieruURIBinding{Port: inbound.Port, Transport: inbound.Transport})
	}
	uri := MieruClientURIWithBindings(clientEndpoint(settings), credential.Username, credential.Password, linkName, bindings)
	return ClientLink{Name: linkName, Protocol: "mieru", Transport: first.Transport, Port: first.Port, URI: uri, Config: config}, true
}

func addMieruClientAccessGroup(groups map[string]*mieruClientAccessGroup, order *[]string, linkName string, credential ClientCredential, inbound Inbound) {
	key := mieruClientAccessGroupKey(linkName, credential)
	group, ok := groups[key]
	if !ok {
		group = &mieruClientAccessGroup{name: linkName, credential: credential}
		groups[key] = group
		*order = append(*order, key)
	}
	group.inbounds = append(group.inbounds, inbound)
}

func mieruClientAccessGroupKey(linkName string, credential ClientCredential) string {
	return linkName + "\x00" + credential.Username + "\x00" + credential.Password
}

func mieruClientAccessProfileLinkName(credential ClientCredential) string {
	name := credential.Name
	if strings.TrimSpace(name) == "" {
		name = credential.Username
	}
	return "mieru/" + name
}
