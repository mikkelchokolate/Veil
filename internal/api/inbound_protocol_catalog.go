package api

import "github.com/veil-panel/veil/internal/protocols"

type InboundProtocolChoice = protocols.Choice
type InboundProtocolCatalog = protocols.Catalog

func NewInboundProtocolCatalog() InboundProtocolCatalog {
	return protocols.NewCatalog()
}
