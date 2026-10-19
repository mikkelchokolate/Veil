package api

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// trafficIdentityMap maps every username still accepted by a live
// per-user inbound (hysteria2, mieru) onto the binding that should receive
// its counters.
//
// Only bindings whose client still passes the render admission gate
// (client.RuntimeEligible at now) may claim an identity: the renderer has
// already removed credentials of disabled, depleted, or expired clients,
// so a residual runtime identity — e.g. a stale hysteria2 /online row —
// must not attribute counters or sessions back to them (#1177).
//
// RuntimeIdentity is canonical. After legacy-profile migration the rendered
// user table still includes the original username so existing URIs keep
// working; the runtime then reports that username in its per-user stats.
// Alias it (and the current client name) onto the migrated binding so
// accounting is not marked degraded for identities the panel itself still
// serves.
//
// The returned keys are matched exactly, which is what mieru needs: its
// usernames are case-sensitive. Hysteria2 callers must use
// hysteria2TrafficIdentityMap instead.
func trafficIdentityMap(inboundName string, profiles []ClientProfile, bindings []client.Binding, clients []client.Client, now int64) map[string]string {
	return buildTrafficIdentityMap(inboundName, profiles, bindings, clients, now, func(identity string) string { return identity })
}

// hysteria2TrafficIdentityMap is trafficIdentityMap in Hysteria2's key space.
// The daemon lowercases every username it authenticates and reports
// (splitUserPass upstream; hy2AuthCallback returns the lowercased ID), and the
// stats provider looks bindings up by strings.ToLower(identity). Folding the
// keys HERE — not only later in hysteria2.NewStatsProvider — keeps the claim
// priority (RuntimeIdentity > migrated legacy username > client name alias)
// intact across case-only collisions: with case-preserved keys, a client
// named "Alice" and another binding's canonical RuntimeIdentity "alice" were
// two distinct entries, and the provider's sorted fold then handed "alice" to
// the name alias because "Alice" sorts first — that binding's sessions and
// traffic were attributed to the wrong client (deviceLimit/ipLimit charged to
// the victim, usage accounting misattributed) (#1282).
func hysteria2TrafficIdentityMap(inboundName string, profiles []ClientProfile, bindings []client.Binding, clients []client.Client, now int64) map[string]string {
	return buildTrafficIdentityMap(inboundName, profiles, bindings, clients, now, strings.ToLower)
}

// buildTrafficIdentityMap implements both maps; key canonicalises every
// identity before it is checked for an existing claim or stored, so first
// claim wins in the runtime's own key space.
func buildTrafficIdentityMap(inboundName string, profiles []ClientProfile, bindings []client.Binding, clients []client.Client, now int64, key func(string) string) map[string]string {
	identities := make(map[string]string)
	clientByID := make(map[string]client.Client, len(clients))
	for _, current := range clients {
		clientByID[current.ID] = current
	}
	inboundBindings := make([]client.Binding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.InboundID != inboundName || !binding.Enabled {
			continue
		}
		current, ok := clientByID[binding.ClientID]
		if !ok || !current.RuntimeEligible(now) {
			continue
		}
		inboundBindings = append(inboundBindings, binding)
		if binding.RuntimeIdentity != "" {
			// RuntimeIdentity is unique per inbound byte-for-byte, but two
			// rows may still collide after folding; the earliest binding
			// (AllBindings is ordered by created_at) keeps the claim.
			identity := key(binding.RuntimeIdentity)
			if _, exists := identities[identity]; !exists {
				identities[identity] = binding.ID
			}
		}
	}

	alias := func(name, bindingID string) {
		name = key(strings.TrimSpace(name))
		if name == "" {
			return
		}
		if _, exists := identities[name]; exists {
			return
		}
		identities[name] = bindingID
	}

	// Migrated legacy usernames claim their identity BEFORE client-name
	// aliases: a normalized client whose name merely equals a legacy username
	// would otherwise steal sessions that still report under it, while the
	// migrated client's own binding (StableClientID) is the row the daemon
	// actually serves that username for (#1225). Unmigrated profiles have no
	// StableClientID binding, so this pass only ever claims migrated ones.
	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		username := strings.TrimSpace(profile.Username)
		if username == "" {
			username = strings.TrimSpace(profile.Name)
		}
		if username == "" {
			continue
		}
		identity := key(username)
		if _, exists := identities[identity]; exists {
			continue
		}
		// StableClientID is derived from the username as stored — fold only
		// the lookup key, never the migration identity.
		wantClient := client.StableClientID(inboundName, username)
		for _, binding := range inboundBindings {
			if binding.ClientID == wantClient {
				identities[identity] = binding.ID
				break
			}
		}
	}
	for _, binding := range inboundBindings {
		if current, ok := clientByID[binding.ClientID]; ok {
			alias(current.Name, binding.ID)
		}
	}
	return identities
}
