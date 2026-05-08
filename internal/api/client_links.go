package api

import (
	"fmt"
	"net/url"
)

// BuildClientLinks creates user-facing client connection links from settings and enabled inbounds.
// Per-inbound passwords override global settings passwords; empty per-inbound passwords fall back
// to the global protocol password for backward compatibility.
func BuildClientLinks(settings Settings, inbounds []Inbound) (ClientLinksResponse, error) {
	if err := NewClientLinksSettingsValidation().Validate(settings); err != nil {
		return ClientLinksResponse{}, err
	}
	response := NewClientLinksResponseMetadata(settings).Build()
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		links, err := buildInboundClientLinks(settings, inbound)
		if err != nil {
			return ClientLinksResponse{}, err
		}
		response.Links = append(response.Links, links...)
	}
	return NewClientLinksResponseFinalizer().Finalize(response)
}

func buildInboundClientLinks(settings Settings, inbound Inbound) ([]ClientLink, error) {
	access, err := BuildClientAccess(settings, inbound)
	if err != nil {
		return nil, err
	}
	return access.ClientLinks(), nil
}

func naiveClientURI(domain string, port int, username string, password string) string {
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("naive+https://%s@%s:%d", userinfo, domain, port)
}

func hysteria2ClientURI(domain string, port int, password string, name string) string {
	query := url.Values{}
	query.Set("sni", domain)
	fragment := url.QueryEscape(name)
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", url.QueryEscape(password), domain, port, query.Encode(), fragment)
}

func hysteria2UserPassClientURI(domain string, port int, username string, password string, name string) string {
	query := url.Values{}
	query.Set("sni", domain)
	fragment := url.QueryEscape(name)
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", userinfo, domain, port, query.Encode(), fragment)
}
