package clientaccess

// ProtocolCapability describes what a protocol can render for clients. The
// clientaccess registry is the single source of truth for client-link
// rendering; this capability surface lets the panel and the per-client
// subscription renderer reason about protocols without hardcoding names.
type ProtocolCapability struct {
	// Protocol is the canonical protocol key (hysteria2, naiveproxy, mieru, olcrtc).
	Protocol string `json:"protocol"`
	// SupportsPerClientCredentials reports whether ProfileLink can render a
	// distinct link per client credential (true for all in-registry protocols).
	SupportsPerClientCredentials bool `json:"supportsPerClientCredentials"`
	// SupportsAggregation reports whether the protocol can combine several
	// inbounds into one artifact (Mieru's combined client config).
	SupportsAggregation bool `json:"supportsAggregation"`
	// SupportsFallback reports whether a credential-less fallback link exists
	// (used when an inbound has no per-client credentials yet).
	SupportsFallback bool `json:"supportsFallback"`
}

// Capability returns the capability descriptor for a protocol, or false when
// the protocol is not in the clientaccess registry (and therefore cannot
// render client links at all).
func (r ClientAccessProtocolRegistry) Capability(protocol string) (ProtocolCapability, bool) {
	p, ok := r.protocols[protocol]
	if !ok {
		return ProtocolCapability{}, false
	}
	return ProtocolCapability{
		Protocol:                     p.Protocol,
		SupportsPerClientCredentials: p.ProfileLink != nil,
		SupportsAggregation:          p.AggregateLinks != nil,
		SupportsFallback:             p.FallbackLink != nil,
	}, true
}

// Capabilities lists capability descriptors for every registered protocol.
func (r ClientAccessProtocolRegistry) Capabilities() []ProtocolCapability {
	out := make([]ProtocolCapability, 0, len(r.protocols))
	for name := range r.protocols {
		if cap, ok := r.Capability(name); ok {
			out = append(out, cap)
		}
	}
	return out
}
