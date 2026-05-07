package api

type InboundProtocolChoice struct {
	Protocol   string   `json:"protocol"`
	Transports []string `json:"transports"`
}

type InboundProtocolCatalog struct {
	choices []InboundProtocolChoice
}

func NewInboundProtocolCatalog() InboundProtocolCatalog {
	return InboundProtocolCatalog{choices: []InboundProtocolChoice{
		{Protocol: "naiveproxy", Transports: []string{"tcp"}},
		{Protocol: "hysteria2", Transports: []string{"udp"}},
		{Protocol: "mieru", Transports: []string{"tcp", "udp"}},
	}}
}

func (c InboundProtocolCatalog) Choices() []InboundProtocolChoice {
	choices := make([]InboundProtocolChoice, len(c.choices))
	for i, choice := range c.choices {
		choices[i] = InboundProtocolChoice{Protocol: choice.Protocol, Transports: append([]string(nil), choice.Transports...)}
	}
	return choices
}

func (c InboundProtocolCatalog) SupportsTransport(protocol, transport string) bool {
	for _, choice := range c.choices {
		if choice.Protocol != protocol {
			continue
		}
		for _, allowed := range choice.Transports {
			if allowed == transport {
				return true
			}
		}
		return false
	}
	return false
}
