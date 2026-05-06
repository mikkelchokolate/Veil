package api

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// BuildClientLinks creates user-facing client connection links from settings and enabled inbounds.
// Per-inbound passwords override global settings passwords; empty per-inbound passwords fall back
// to the global protocol password for backward compatibility.
func BuildClientLinks(settings Settings, inbounds []Inbound) (ClientLinksResponse, error) {
	if strings.TrimSpace(settings.Domain) == "" {
		return ClientLinksResponse{}, errors.New("domain is required to build client links")
	}
	response := ClientLinksResponse{
		SchemaVersion:              "v1",
		Domain:                     settings.Domain,
		Stack:                      settings.Stack,
		SubscriptionURL:            "/api/client-links/subscription",
		Base64SubscriptionURL:      "/api/client-links/subscription?format=base64",
		RawSubscriptionURL:         "/api/client-links/subscription?format=raw",
		DefaultSubscriptionFormat:  "base64",
		Base64SubscriptionFilename: "veil-subscription.txt",
		RawSubscriptionFilename:    "veil-subscription-raw.txt",
		SubscriptionContentType:    "text/plain; charset=utf-8",
		SubscriptionFormats:        []string{"base64", "raw"},
	}
	for _, inbound := range inbounds {
		if !inbound.Enabled || !stackAllowsProtocol(settings.Stack, inbound.Protocol) {
			continue
		}
		links, err := buildInboundClientLinks(settings, inbound)
		if err != nil {
			return ClientLinksResponse{}, err
		}
		response.Links = append(response.Links, links...)
	}
	if len(response.Links) == 0 {
		return ClientLinksResponse{}, errors.New("no enabled client links are available")
	}
	response.Count = len(response.Links)
	return response, nil
}

func buildInboundClientLinks(settings Settings, inbound Inbound) ([]ClientLink, error) {
	profiles := enabledClientProfiles(inbound)
	if len(profiles) == 0 {
		return []ClientLink{fallbackInboundClientLink(settings, inbound)}, nil
	}
	links := make([]ClientLink, 0, len(profiles))
	for _, profile := range profiles {
		link := ClientLink{Name: inbound.Name + "/" + profile.Name, Protocol: inbound.Protocol, Transport: inbound.Transport, Port: inbound.Port}
		switch inbound.Protocol {
		case "naiveproxy":
			username := profile.Username
			if username == "" {
				username = profile.Name
			}
			if username == "" || profile.Password == "" {
				return nil, errors.New("naive client profile username and password are required to build client links")
			}
			link.URI = naiveClientURI(settings.Domain, inbound.Port, username, profile.Password)
		case "hysteria2":
			username := profile.Username
			if username == "" {
				username = profile.Name
			}
			if username == "" || profile.Password == "" {
				return nil, errors.New("hysteria2 client profile username and password are required to build client links")
			}
			link.URI = hysteria2UserPassClientURI(settings.Domain, inbound.Port, username, profile.Password, link.Name)
		default:
			continue
		}
		links = append(links, link)
	}
	return links, nil
}

func enabledClientProfiles(inbound Inbound) []ClientProfile {
	profiles := []ClientProfile{}
	for _, profile := range inbound.Profiles {
		if profile.Enabled {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

func fallbackInboundClientLink(settings Settings, inbound Inbound) ClientLink {
	link := ClientLink{Name: inbound.Name, Protocol: inbound.Protocol, Transport: inbound.Transport, Port: inbound.Port}
	switch inbound.Protocol {
	case "naiveproxy":
		password := inbound.Password
		if password == "" {
			password = settings.NaivePassword
		}
		link.URI = naiveClientURI(settings.Domain, inbound.Port, settings.NaiveUsername, password)
	case "hysteria2":
		password := inbound.Password
		if password == "" {
			password = settings.Hysteria2Password
		}
		link.URI = hysteria2ClientURI(settings.Domain, inbound.Port, password, inbound.Name)
	}
	return link
}

func stackAllowsProtocol(stack string, protocol string) bool {
	switch stack {
	case "naive":
		return protocol == "naiveproxy"
	case "hysteria2":
		return protocol == "hysteria2"
	default:
		return true
	}
}

func naiveClientURI(domain string, port int, username string, password string) string {
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("https://%s@%s:%d", userinfo, domain, port)
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
