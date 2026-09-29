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
func trafficIdentityMap(inboundName string, profiles []ClientProfile, bindings []client.Binding, clients []client.Client, now int64) map[string]string {
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
			identities[binding.RuntimeIdentity] = binding.ID
		}
	}

	alias := func(name, bindingID string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := identities[name]; exists {
			return
		}
		identities[name] = bindingID
	}

	for _, binding := range inboundBindings {
		if current, ok := clientByID[binding.ClientID]; ok {
			alias(current.Name, binding.ID)
		}
	}
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
		if _, exists := identities[username]; exists {
			continue
		}
		wantClient := client.StableClientID(inboundName, username)
		for _, binding := range inboundBindings {
			if binding.ClientID == wantClient {
				identities[username] = binding.ID
				break
			}
		}
	}
	return identities
}
