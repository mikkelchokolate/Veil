package naiveproxy

import (
	"errors"
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// BuildLinks creates client links for a naiveproxy inbound.
func (p Plugin) BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	return BuildLinks(settings, inbound)
}

// BuildLinks creates client links for a naiveproxy inbound based on its
// configured transport. TCP yields an https:// URI, QUIC yields a quic:// URI,
// and dual yields both. The port is omitted when it matches the default (443).
func BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	domain := NaiveDomain(settings, inbound)
	if domain == "" {
		return nil, nil
	}
	port := NaivePublicPort(settings, inbound)
	transport := NaiveTransport(inbound)
	creds := inbound.Profiles
	if len(creds) == 0 {
		return nil, errors.New("no profiles")
	}
	var links []model.ClientLink
	for _, profile := range creds {
		if transport == "tcp" || transport == "dual" {
			links = append(links, model.ClientLink{
				Name:      inbound.Name + "-https",
				Protocol:  "naiveproxy",
				Transport: "tcp",
				Port:      port,
				URI:       naiveURI("https", profile.Username, profile.Password, domain, port, 443),
			})
		}
		if transport == "quic" || transport == "dual" {
			links = append(links, model.ClientLink{
				Name:      inbound.Name + "-quic",
				Protocol:  "naiveproxy",
				Transport: "quic",
				Port:      port,
				URI:       naiveURI("quic", profile.Username, profile.Password, domain, port, 443),
			})
		}
	}
	return links, nil
}

func naiveURI(scheme, user, pass, domain string, port, defaultPort int) string {
	host := domain
	if port != defaultPort {
		host = fmt.Sprintf("%s:%d", domain, port)
	}
	return fmt.Sprintf("%s://%s:%s@%s", scheme, user, pass, host)
}
