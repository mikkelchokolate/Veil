package naiveproxy

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// BuildLinks creates client links for a naiveproxy inbound.
func (p Plugin) BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	endpoint := clientEndpoint(settings)
	if endpoint == "" {
		return nil, nil
	}
	creds, err := clientaccess.BuildClientCredentials(inbound)
	if err != nil {
		return nil, err
	}
	links := make([]model.ClientLink, 0, len(creds))
	if len(creds) == 0 {
		link := model.ClientLink{
			Name:      inbound.Name,
			Protocol:  inbound.Protocol,
			Transport: inbound.Transport,
			Port:      inbound.Port,
			URI:       clientaccess.NaiveClientURI(endpoint, inbound.Port, p.naiveFallbackUsername(settings, inbound), p.naiveFallbackPassword(settings, inbound)),
		}
		return []model.ClientLink{link}, nil
	}
	for _, cred := range creds {
		link := model.ClientLink{
			Name:      inbound.Name + "/" + cred.Name,
			Protocol:  inbound.Protocol,
			Transport: inbound.Transport,
			Port:      inbound.Port,
			URI:       clientaccess.NaiveClientURI(endpoint, inbound.Port, cred.Username, cred.Password),
		}
		links = append(links, link)
	}
	return links, nil
}

func (Plugin) naiveFallbackUsername(settings model.Settings, inbound model.Inbound) string {
	username := naiveUsername(settings, inbound)
	if username == "" {
		username = settings.NaiveUsername
	}
	return username
}

func (Plugin) naiveFallbackPassword(settings model.Settings, inbound model.Inbound) string {
	password := naivePassword(settings, inbound)
	if password == "" {
		password = settings.NaivePassword
	}
	return password
}

func clientEndpoint(settings model.Settings) string {
	return strings.TrimSpace(settings.Domain)
}
