package inbounds

import (
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type Inbound = model.Inbound
type ClientProfile = model.ClientProfile

type ProtocolCatalog = protocols.Catalog

type InboundCredentialPolicy = clientaccess.InboundCredentialPolicy

func NewCatalog(inbounds []Inbound) InboundCatalog { return NewInboundCatalog(inbounds) }

func NewCatalogWithPasswordGenerator(inbounds []Inbound, generator InboundPasswordGenerator) InboundCatalog {
	return NewInboundCatalogWithPasswordGenerator(inbounds, generator)
}

func NewInboundProtocolCatalog() ProtocolCatalog { return protocols.NewCatalog() }

func NewInboundCredentialPolicy(generate InboundPasswordGenerator) InboundCredentialPolicy {
	return clientaccess.NewInboundCredentialPolicy(clientaccess.InboundPasswordGenerator(generate))
}

func generateInboundPassword() string {
	return clientaccess.NewManagementPasswordGenerator(nil).Generate()
}
