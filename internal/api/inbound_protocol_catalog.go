package api

type InboundProtocolChoice struct {
	Protocol        string   `json:"protocol"`
	DisplayName     string   `json:"displayName"`
	Transports      []string `json:"transports"`
	FirewallService string   `json:"firewallService"`
}

type InboundProtocolCatalog struct {
	choices []InboundProtocolChoice
}

func NewInboundProtocolCatalog() InboundProtocolCatalog {
	return InboundProtocolCatalog{choices: []InboundProtocolChoice{
		{Protocol: "naiveproxy", DisplayName: "NaiveProxy", Transports: []string{"tcp"}, FirewallService: "Veil NaiveProxy"},
		{Protocol: "hysteria2", DisplayName: "Hysteria2", Transports: []string{"udp"}, FirewallService: "Veil Hysteria2"},
		{Protocol: "mieru", DisplayName: "Mieru", Transports: []string{"tcp", "udp"}, FirewallService: "Veil Mieru"},
	}}
}

func (c InboundProtocolCatalog) Choices() []InboundProtocolChoice {
	choices := make([]InboundProtocolChoice, len(c.choices))
	for i, choice := range c.choices {
		choices[i] = InboundProtocolChoice{Protocol: choice.Protocol, DisplayName: choice.DisplayName, Transports: append([]string(nil), choice.Transports...), FirewallService: choice.FirewallService}
	}
	return choices
}

func (c InboundProtocolCatalog) DisplayNameList() string {
	names := make([]string, 0, len(c.choices))
	for _, choice := range c.choices {
		names = append(names, choice.DisplayName)
	}
	return englishList(names)
}

func (c InboundProtocolCatalog) FirewallService(protocol string) (string, bool) {
	choice, ok := c.choice(protocol)
	if !ok || choice.FirewallService == "" {
		return "", false
	}
	return choice.FirewallService, true
}

func (c InboundProtocolCatalog) Supports(protocol string) bool {
	_, ok := c.choice(protocol)
	return ok
}

func (c InboundProtocolCatalog) SupportsTransport(protocol, transport string) bool {
	choice, ok := c.choice(protocol)
	if !ok {
		return false
	}
	for _, allowed := range choice.Transports {
		if allowed == transport {
			return true
		}
	}
	return false
}

func (c InboundProtocolCatalog) choice(protocol string) (InboundProtocolChoice, bool) {
	for _, choice := range c.choices {
		if choice.Protocol == protocol {
			return choice, true
		}
	}
	return InboundProtocolChoice{}, false
}

func englishList(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " and " + values[1]
	default:
		out := ""
		for i, value := range values {
			if i > 0 {
				if i == len(values)-1 {
					out += ", and "
				} else {
					out += ", "
				}
			}
			out += value
		}
		return out
	}
}
