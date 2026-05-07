package api

type InboundValidation struct{}

func NewInboundValidation() InboundValidation { return InboundValidation{} }

func (InboundValidation) ValidateCreate(inbound Inbound) error {
	if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || inbound.Port <= 0 {
		return ErrInboundInvalid
	}
	return nil
}

func (InboundValidation) ValidateUpdate(inbound Inbound) error {
	if inbound.Protocol == "" || inbound.Transport == "" || inbound.Port <= 0 {
		return ErrInboundInvalid
	}
	return nil
}

func validateInboundForCreate(inbound Inbound) error {
	return NewInboundValidation().ValidateCreate(inbound)
}

func validateInboundForUpdate(inbound Inbound) error {
	return NewInboundValidation().ValidateUpdate(inbound)
}
