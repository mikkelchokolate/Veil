package hysteria2

import (
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// BuildLinks creates client links for a Hysteria2 inbound.
func (p Plugin) BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	endpoint := hysteria2Domain(settings, inbound)
	if endpoint == "" {
		return nil, nil
	}
	creds, err := clientaccess.BuildClientCredentials(inbound)
	if err != nil {
		return nil, err
	}
	insecure := hysteria2Insecure(settings, inbound)
	if len(creds) == 0 {
		password := hysteria2Password(settings, inbound)
		link := model.ClientLink{
			Name:      inbound.Name,
			Protocol:  inbound.Protocol,
			Transport: inbound.Transport,
			Port:      inbound.Port,
			URI:       clientaccess.Hysteria2ClientURI(endpoint, inbound.Port, password, inbound.Name, insecure),
		}
		return []model.ClientLink{link}, nil
	}
	links := make([]model.ClientLink, 0, len(creds))
	for _, cred := range creds {
		link := model.ClientLink{
			Name:      inbound.Name + "/" + cred.Name,
			Protocol:  inbound.Protocol,
			Transport: inbound.Transport,
			Port:      inbound.Port,
			URI:       clientaccess.Hysteria2UserPassClientURI(endpoint, inbound.Port, cred.Username, cred.Password, cred.Name, insecure),
		}
		links = append(links, link)
	}
	return links, nil
}
