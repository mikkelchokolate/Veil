package protocols

// TelemetrySupport describes the per-client traffic observability a
// protocol's rendered runtime can provide. It is the single source of truth
// shared by the binding capability surface (BindingCapability), the quota
// write validator, and the /api/protocols catalog — so the API can never
// advertise a capability the write path would reject.
type TelemetrySupport struct {
	// TrafficAccounting: the runtime exposes per-user byte counters the
	// traffic collector can read (hysteria2 stats endpoint, mita metrics).
	TrafficAccounting bool `json:"trafficAccounting"`
	// QuotaEnforcement: the rendered runtime actually rejects a depleted
	// client's traffic. Only hysteria2 qualifies today — mieru counts
	// per-user traffic but its rendered user table carries no quota limits.
	QuotaEnforcement bool `json:"quotaEnforcement"`
	// DeviceLimits: the runtime admits each session through a protocol-level
	// authentication hook that enforces a client's deviceLimit (concurrent
	// sessions) and ipLimit (distinct source IPs). Only hysteria2 qualifies:
	// its rendered config switches auth to the internal HTTP callback the
	// panel serves, which checks both limits before accepting a session.
	DeviceLimits bool `json:"deviceLimits"`
}

// TelemetrySupportOf returns the telemetry capability of a protocol, keyed by
// canonical protocol name; unknown protocols report no support.
func TelemetrySupportOf(protocol string) TelemetrySupport {
	switch protocol {
	case "hysteria2":
		return TelemetrySupport{TrafficAccounting: true, QuotaEnforcement: true, DeviceLimits: true}
	case "mieru":
		return TelemetrySupport{TrafficAccounting: true}
	}
	return TelemetrySupport{}
}
