package inbounds

type InboundTransportPortIndex struct {
	inbounds []Inbound
}

func NewInboundTransportPortIndex(inbounds []Inbound) InboundTransportPortIndex {
	return InboundTransportPortIndex{inbounds: inbounds}
}

func (i InboundTransportPortIndex) Has(transport string, port int, exceptIndex int) bool {
	for idx, existing := range i.inbounds {
		if idx == exceptIndex {
			continue
		}
		if existing.Transport == transport && existing.Port == port {
			return true
		}
	}
	return false
}
