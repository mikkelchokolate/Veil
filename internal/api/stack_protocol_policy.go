package api

type StackProtocolPolicy struct {
	stack string
}

func NewStackProtocolPolicy(stack string) StackProtocolPolicy {
	return StackProtocolPolicy{stack: stack}
}

func (p StackProtocolPolicy) Includes(protocol string) bool {
	return NewStackSelectionCatalog().IncludesProtocol(p.stack, protocol)
}

func stackIncludesProtocol(stack string, protocol string) bool {
	return NewStackProtocolPolicy(stack).Includes(protocol)
}
