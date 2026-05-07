package api

type ClientLinkStackPolicy struct {
	stack string
}

func NewClientLinkStackPolicy(stack string) ClientLinkStackPolicy {
	return ClientLinkStackPolicy{stack: stack}
}

func (p ClientLinkStackPolicy) Allows(protocol string) bool {
	switch p.stack {
	case "naive":
		return protocol == "naiveproxy"
	case "hysteria2":
		return protocol == "hysteria2"
	default:
		return true
	}
}

func stackAllowsProtocol(stack string, protocol string) bool {
	return NewClientLinkStackPolicy(stack).Allows(protocol)
}
