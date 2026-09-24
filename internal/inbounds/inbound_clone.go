package inbounds

import "github.com/mikkelchokolate/Veil/internal/model"

type InboundClone struct{}

func NewInboundClone() InboundClone { return InboundClone{} }

// Slice returns a deep copy of the inbound list: profiles, runtime
// credentials, and every nested protocol-fields container are detached so a
// mutation on the clone can never alias into the source.
func (InboundClone) Slice(inbounds []Inbound) []Inbound {
	out := make([]Inbound, len(inbounds))
	for idx, inbound := range inbounds {
		out[idx] = model.CloneInbound(inbound)
	}
	return out
}

func cloneInbounds(inbounds []Inbound) []Inbound {
	return NewInboundClone().Slice(inbounds)
}
