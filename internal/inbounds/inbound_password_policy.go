package inbounds

type InboundPasswordPolicy struct {
	generate InboundPasswordGenerator
}

func NewInboundPasswordPolicy(generate InboundPasswordGenerator) InboundPasswordPolicy {
	if generate == nil {
		generate = generateInboundPassword
	}
	return InboundPasswordPolicy{generate: generate}
}

func (p InboundPasswordPolicy) ApplyCreate(inbound *Inbound) {
	if inbound.Password == "" && len(inbound.Profiles) == 0 {
		inbound.Password = p.generate()
	}
}

func (InboundPasswordPolicy) ApplyUpdate(inbound *Inbound, previous Inbound) {
	if inbound.Password == "" {
		inbound.Password = previous.Password
	}
}
