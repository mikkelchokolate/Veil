package naiveproxy

import (
	"fmt"
	"net/url"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// BuildLinks creates client links for a naiveproxy inbound.
func (p Plugin) BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	return BuildLinks(settings, inbound)
}

// BuildLinks creates client links for a naiveproxy inbound based on its
// configured transport. TCP yields an https:// URI, QUIC yields a quic:// URI,
// and dual yields both. The port is omitted when it matches the default (443).
// Only enabled profiles are exported, and the effective public port is used,
// matching the registry path (audit #79/#124/#130).
func BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	domain := NaiveDomain(settings, inbound)
	if domain == "" {
		return nil, nil
	}
	port := NaivePublicPort(settings, inbound)
	transport := NaiveTransport(inbound)
	resolved, err := clientaccess.BuildClientCredentials(inbound)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		username := naiveUsername(settings, inbound)
		password := naivePassword(settings, inbound)
		if username == "" || password == "" {
			return nil, fmt.Errorf("no profiles")
		}
		resolved = []clientaccess.ClientCredential{{Name: inbound.Name, Username: username, Password: password}}
	}
	var links []model.ClientLink
	for _, profile := range resolved {
		name := inbound.Name
		if profile.Name != "" && profile.Name != inbound.Name {
			name = inbound.Name + "/" + profile.Name
		}
		username := profile.Username
		if username == "" {
			username = profile.Name
		}
		if transport == "tcp" || transport == "dual" {
			links = append(links, model.ClientLink{
				Name:      name,
				Protocol:  "naiveproxy",
				Transport: "tcp",
				Port:      port,
				URI:       naiveURI("https", username, profile.Password, domain, port, 443),
			})
		}
		if transport == "quic" || transport == "dual" {
			links = append(links, model.ClientLink{
				Name:      name + "-quic",
				Protocol:  "naiveproxy",
				Transport: "quic",
				Port:      port,
				URI:       naiveURI("quic", username, profile.Password, domain, port, 443),
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
	// Userinfo must be percent-encoded (RFC 3986): raw interpolation lets a
	// username/password containing '@' or ':' redirect the URI to another
	// host or break parsing (audit #191, red-team verified).
	userinfo := url.UserPassword(user, pass).String()
	return fmt.Sprintf("%s://%s@%s", scheme, userinfo, host)
}
