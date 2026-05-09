package inbounds

import (
	"github.com/veil-panel/veil/internal/clientaccess"
	"github.com/veil-panel/veil/internal/model"
	"github.com/veil-panel/veil/internal/protocols"
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
