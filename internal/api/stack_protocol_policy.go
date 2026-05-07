package api

type StackProtocolPolicy struct {
	stack string
}

func NewStackProtocolPolicy(stack string) StackProtocolPolicy {
	return StackProtocolPolicy{stack: stack}
}

func (p StackProtocolPolicy) Includes(protocol string) bool {
	switch p.stack {
	case "both":
		return true
	case "naive":
		return protocol == "naiveproxy"
	case "hysteria2":
		return protocol == "hysteria2"
	case "mieru":
		return protocol == "mieru"
	case "panel":
		return false
	default:
		return false
	}
}

func stackIncludesProtocol(stack string, protocol string) bool {
	return NewStackProtocolPolicy(stack).Includes(protocol)
}
