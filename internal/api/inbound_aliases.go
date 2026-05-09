package api

import "github.com/veil-panel/veil/internal/inbounds"

var (
	ErrInboundInvalid                      = inbounds.ErrInboundInvalid
	ErrInboundNotFound                     = inbounds.ErrInboundNotFound
	ErrInboundDuplicateName                = inbounds.ErrInboundDuplicateName
	ErrInboundDuplicateTransportPort       = inbounds.ErrInboundDuplicateTransportPort
	ErrInboundUnsupportedProtocolTransport = inbounds.ErrInboundUnsupportedProtocolTransport
)

type InboundPasswordGenerator = inbounds.InboundPasswordGenerator
type InboundCatalog = inbounds.InboundCatalog
type InboundClone = inbounds.InboundClone
type InboundValidation = inbounds.InboundValidation
type InboundTransportPortIndex = inbounds.InboundTransportPortIndex
type InboundPasswordPolicy = inbounds.InboundPasswordPolicy

func NewInboundCatalog(values []Inbound) InboundCatalog {
	return inbounds.NewInboundCatalog(values)
}

func NewInboundCatalogWithPasswordGenerator(values []Inbound, generator InboundPasswordGenerator) InboundCatalog {
	return inbounds.NewInboundCatalogWithPasswordGenerator(values, generator)
}

func NewInboundClone() InboundClone { return inbounds.NewInboundClone() }

func NewInboundValidation() InboundValidation { return inbounds.NewInboundValidation() }

func NewInboundTransportPortIndex(values []Inbound) InboundTransportPortIndex {
	return inbounds.NewInboundTransportPortIndex(values)
}

func NewInboundPasswordPolicy(generator InboundPasswordGenerator) InboundPasswordPolicy {
	return inbounds.NewInboundPasswordPolicy(generator)
}

func validateInboundForCreate(inbound Inbound) error {
	return inbounds.NewInboundValidation().ValidateCreate(inbound)
}

func validateInboundForUpdate(inbound Inbound) error {
	return inbounds.NewInboundValidation().ValidateUpdate(inbound)
}

func cloneInbounds(values []Inbound) []Inbound {
	return inbounds.NewInboundClone().Slice(values)
}
