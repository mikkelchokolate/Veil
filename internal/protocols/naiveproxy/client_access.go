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

	profiles := make([]model.ClientProfile, 0, len(inbound.Profiles)+1)
	hasExplicitlyEnabledProfile := false
	for _, profile := range inbound.Profiles {
		if profile.Enabled {
			hasExplicitlyEnabledProfile = true
			break
		}
	}
	for _, profile := range inbound.Profiles {
		if hasExplicitlyEnabledProfile && !profile.Enabled {
			continue
		}
		username := profile.Username
		if username == "" {
			username = profile.Name
		}
		if username == "" || profile.Password == "" {
			return nil, errors.New("enabled profile requires username and password")
		}
		profile.Username = username
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		username := naiveUsername(settings, inbound)
		password := naivePassword(settings, inbound)
		if username == "" || password == "" {
			return nil, errors.New("no profiles")
		}
		profiles = append(profiles, model.ClientProfile{Name: inbound.Name, Username: username, Password: password, Enabled: true})
	}

	var links []model.ClientLink
	for _, profile := range profiles {
		baseName := inbound.Name
		if profile.Name != "" && profile.Name != inbound.Name {
			baseName += "/" + profile.Name
		}
		if transport == "tcp" || transport == "dual" {
			name := baseName
			if transport == "dual" {
				name += "-https"
			}
			links = append(links, model.ClientLink{Name: name, Protocol: "naiveproxy", Transport: "tcp", Port: port, URI: naiveURI("https", profile.Username, profile.Password, domain, port, 443)})
		}
		if transport == "quic" || transport == "dual" {
			name := baseName
			if transport == "dual" {
				name += "-quic"
			}
			links = append(links, model.ClientLink{Name: name, Protocol: "naiveproxy", Transport: "quic", Port: port, URI: naiveURI("quic", profile.Username, profile.Password, domain, port, 443)})
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
