package inbounds

import "regexp"

var inboundNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type InboundValidation struct{}

func NewInboundValidation() InboundValidation { return InboundValidation{} }

func IsSafeName(name string) bool {
	return inboundNamePattern.MatchString(name)
}

func (InboundValidation) ValidateCreate(inbound Inbound) error {
	if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || inbound.Port <= 0 {
		return ErrInboundInvalid
	}
	if !IsSafeName(inbound.Name) {
		return ErrInboundInvalid
	}
	if !NewInboundProtocolCatalog().SupportsTransport(inbound.Protocol, inbound.Transport) {
		return ErrInboundUnsupportedProtocolTransport
	}
	return nil
}

func (InboundValidation) ValidateUpdate(inbound Inbound) error {
	if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || inbound.Port <= 0 {
		return ErrInboundInvalid
	}
	if !IsSafeName(inbound.Name) {
		return ErrInboundInvalid
	}
	if !NewInboundProtocolCatalog().SupportsTransport(inbound.Protocol, inbound.Transport) {
		return ErrInboundUnsupportedProtocolTransport
	}
	return nil
}

func validateInboundForCreate(inbound Inbound) error {
	return NewInboundValidation().ValidateCreate(inbound)
}

func validateInboundForUpdate(inbound Inbound) error {
	return NewInboundValidation().ValidateUpdate(inbound)
}
