package inbounds

import "regexp"

var inboundNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type InboundValidation struct{}

func NewInboundValidation() InboundValidation { return InboundValidation{} }

func IsSafeName(name string) bool {
	return inboundNamePattern.MatchString(name)
}

func (InboundValidation) ValidateCreate(inbound Inbound) error {
	if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || !validPort(inbound.Port) {
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
	if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || !validPort(inbound.Port) {
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

// validPort reports whether a TCP/UDP port is in the valid range [1, 65535].
// The previous check only rejected ports <= 0, so values like 70000 sailed
// through validation into the renderer as "listen: :70000" (audit #21/#98).
func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validateInboundForCreate(inbound Inbound) error {
	return NewInboundValidation().ValidateCreate(inbound)
}

func validateInboundForUpdate(inbound Inbound) error {
	return NewInboundValidation().ValidateUpdate(inbound)
}
