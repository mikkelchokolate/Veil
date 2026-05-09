package clientaccess

import "strings"

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
			if inbound.Password == "" {
				continue
			}
			credential := ClientCredential{Name: inbound.Name, Username: inbound.Name, Password: inbound.Password}
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
		config, err := NewMieruClientConfig().BuildWithBindings(settings, group.inbounds, group.name, group.credential)
		if err != nil {
			continue
		}
		first := group.inbounds[0]
		links = append(links, ClientLink{Name: group.name, Protocol: "mieru", Transport: first.Transport, Port: first.Port, Config: config})
	}
	return links, nil
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
