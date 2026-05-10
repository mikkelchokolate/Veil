package api

import (
	"net/url"

	"github.com/veil-panel/veil/internal/clientaccess"
)

type ClientCredential = clientaccess.ClientCredential
type ClientAccessLinkInput = clientaccess.ClientAccessLinkInput

func BuildClientLinks(settings Settings, inbounds []Inbound) (ClientLinksResponse, error) {
	return clientaccess.BuildClientLinks(settings, inbounds)
}

func NewClientLinkDeliveryHeaders() clientaccess.ClientLinkDeliveryHeaders {
	return clientaccess.NewClientLinkDeliveryHeaders()
}

func ValidateClientSubscriptionQuery(query url.Values) error {
	return clientaccess.ValidateClientSubscriptionQuery(query)
}

func BuildClientSubscription(response ClientLinksResponse, format string) (clientaccess.ClientSubscription, error) {
	return clientaccess.BuildClientSubscription(response, format)
}

func NewClientSubscriptionDeliveryHeaders(subscription clientaccess.ClientSubscription) clientaccess.ClientSubscriptionDeliveryHeaders {
	return clientaccess.NewClientSubscriptionDeliveryHeaders(subscription)
}

func NewMieruClientAccessAggregator() clientaccess.MieruClientAccessAggregator {
	return clientaccess.NewMieruClientAccessAggregator()
}

func NewMieruClientConfig() clientaccess.MieruClientConfig {
	return clientaccess.NewMieruClientConfig()
}

func naiveClientURI(domain string, port int, username string, password string) string {
	return clientaccess.NaiveClientURI(domain, port, username, password)
}

func hysteria2ClientURI(domain string, port int, password string, name string) string {
	return clientaccess.Hysteria2ClientURI(domain, port, password, name)
}

func hysteria2UserPassClientURI(domain string, port int, username string, password string, name string) string {
	return clientaccess.Hysteria2UserPassClientURI(domain, port, username, password, name)
}

func newProtocolClientLink(input ClientAccessLinkInput) ClientLink {
	return ClientLink{Name: input.LinkName, Protocol: input.Inbound.Protocol, Transport: input.Inbound.Transport, Port: input.Inbound.Port}
}

func mieruClientConfigLink(input ClientAccessLinkInput) (ClientLink, bool) {
	link := newProtocolClientLink(input)
	config, err := NewMieruClientConfig().Build(input.Settings, input.Inbound, link.Name, input.Credential)
	if err != nil {
		return ClientLink{}, false
	}
	link.Config = config
	return link, true
}
