package api

import (
	"net/url"

	"github.com/veil-panel/veil/internal/clientaccess"
)

type ClientAccess = clientaccess.ClientAccess
type ClientCredential = clientaccess.ClientCredential
type ClientAccessProtocolRegistry = clientaccess.ClientAccessProtocolRegistry
type ClientAccessProtocol = clientaccess.ClientAccessProtocol
type ClientAccessLinkInput = clientaccess.ClientAccessLinkInput
type ClientAccessDelivery = clientaccess.ClientAccessDelivery
type ClientLinksResponseFinalizer = clientaccess.ClientLinksResponseFinalizer
type ClientLinksResponseMetadata = clientaccess.ClientLinksResponseMetadata
type ClientLinksSettingsValidation = clientaccess.ClientLinksSettingsValidation
type ClientProfileCatalog = clientaccess.ClientProfileCatalog
type ClientProfilePasswordPolicy = clientaccess.ClientProfilePasswordPolicy
type ClientLinkDeliveryHeaders = clientaccess.ClientLinkDeliveryHeaders
type ClientSubscription = clientaccess.ClientSubscription
type ClientSubscriptionPayload = clientaccess.ClientSubscriptionPayload
type ClientSubscriptionFormatPolicy = clientaccess.ClientSubscriptionFormatPolicy
type ClientSubscriptionDeliveryHeaders = clientaccess.ClientSubscriptionDeliveryHeaders
type MieruClientAccessAggregator = clientaccess.MieruClientAccessAggregator
type MieruClientConfig = clientaccess.MieruClientConfig
type InboundCredentialPolicy = clientaccess.InboundCredentialPolicy

func BuildClientAccess(settings Settings, inbound Inbound) (ClientAccess, error) {
	return clientaccess.BuildClientAccess(settings, inbound)
}

func BuildClientCredentials(inbound Inbound) ([]ClientCredential, error) {
	return clientaccess.BuildClientCredentials(inbound)
}

func BuildClientLinks(settings Settings, inbounds []Inbound) (ClientLinksResponse, error) {
	return clientaccess.BuildClientLinks(settings, inbounds)
}

func NewClientAccessProtocolRegistry() ClientAccessProtocolRegistry {
	return clientaccess.NewClientAccessProtocolRegistry()
}

func NewClientAccessDelivery(response ClientLinksResponse) ClientAccessDelivery {
	return clientaccess.NewClientAccessDelivery(response)
}

func NewClientLinksResponseFinalizer() ClientLinksResponseFinalizer {
	return clientaccess.NewClientLinksResponseFinalizer()
}

func NewClientLinksResponseMetadata(settings Settings) ClientLinksResponseMetadata {
	return clientaccess.NewClientLinksResponseMetadata(settings)
}

func NewClientLinksSettingsValidation() ClientLinksSettingsValidation {
	return clientaccess.NewClientLinksSettingsValidation()
}

func NewClientProfileCatalog(profiles []ClientProfile) ClientProfileCatalog {
	return clientaccess.NewClientProfileCatalog(profiles)
}

func NewClientProfileCatalogWithPasswordGenerator(profiles []ClientProfile, generator InboundPasswordGenerator) ClientProfileCatalog {
	return clientaccess.NewClientProfileCatalogWithPasswordGenerator(profiles, clientaccess.InboundPasswordGenerator(generator))
}

func NewClientProfilePasswordPolicy(generate InboundPasswordGenerator) ClientProfilePasswordPolicy {
	return clientaccess.NewClientProfilePasswordPolicy(clientaccess.InboundPasswordGenerator(generate))
}

func NewClientLinkDeliveryHeaders() ClientLinkDeliveryHeaders {
	return clientaccess.NewClientLinkDeliveryHeaders()
}

func NewClientSubscriptionPayload(response ClientLinksResponse) ClientSubscriptionPayload {
	return clientaccess.NewClientSubscriptionPayload(response)
}

func ValidateClientSubscriptionQuery(query url.Values) error {
	return clientaccess.ValidateClientSubscriptionQuery(query)
}

func BuildClientSubscription(response ClientLinksResponse, format string) (ClientSubscription, error) {
	return clientaccess.BuildClientSubscription(response, format)
}

func NewClientSubscriptionFormatPolicy() ClientSubscriptionFormatPolicy {
	return clientaccess.NewClientSubscriptionFormatPolicy()
}

func NewClientSubscriptionDeliveryHeaders(subscription ClientSubscription) ClientSubscriptionDeliveryHeaders {
	return clientaccess.NewClientSubscriptionDeliveryHeaders(subscription)
}

func NewMieruClientAccessAggregator() MieruClientAccessAggregator {
	return clientaccess.NewMieruClientAccessAggregator()
}

func NewMieruClientConfig() MieruClientConfig { return clientaccess.NewMieruClientConfig() }

func NewInboundCredentialPolicy(generate InboundPasswordGenerator) InboundCredentialPolicy {
	return clientaccess.NewInboundCredentialPolicy(clientaccess.InboundPasswordGenerator(generate))
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
