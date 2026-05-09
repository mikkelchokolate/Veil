package inbounds

type InboundClone struct{}

func NewInboundClone() InboundClone { return InboundClone{} }

func (InboundClone) Slice(inbounds []Inbound) []Inbound {
	out := make([]Inbound, len(inbounds))
	for idx, inbound := range inbounds {
		out[idx] = inbound
		out[idx].Profiles = append([]ClientProfile(nil), inbound.Profiles...)
	}
	return out
}

func cloneInbounds(inbounds []Inbound) []Inbound {
	return NewInboundClone().Slice(inbounds)
}
