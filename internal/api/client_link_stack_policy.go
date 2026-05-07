package api

type ClientLinkStackPolicy struct {
	stack string
}

func NewClientLinkStackPolicy(stack string) ClientLinkStackPolicy {
	return ClientLinkStackPolicy{stack: stack}
}

func (p ClientLinkStackPolicy) Allows(protocol string) bool {
	if p.stack == "" || p.stack == "unknown" {
		return true
	}
	return NewStackProtocolPolicy(p.stack).Includes(protocol)
}

func stackAllowsProtocol(stack string, protocol string) bool {
	return NewClientLinkStackPolicy(stack).Allows(protocol)
}
