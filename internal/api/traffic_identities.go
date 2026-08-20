package api

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// hysteria2TrafficIdentityMap maps every username still accepted by a live
// Hysteria2 inbound onto the binding that should receive its counters.
//
// RuntimeIdentity is canonical. After legacy-profile migration the rendered
// auth table still includes the original username so existing URIs keep
// working; Hysteria2 then reports that username in Traffic Stats. Alias it
// (and the current client name) onto the migrated binding so accounting is
// not marked degraded for identities the panel itself still serves.
func hysteria2TrafficIdentityMap(inboundName string, profiles []ClientProfile, bindings []client.Binding, clients []client.Client) map[string]string {
	identities := make(map[string]string)
	inboundBindings := make([]client.Binding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.InboundID != inboundName || !binding.Enabled {
			continue
		}
		inboundBindings = append(inboundBindings, binding)
		if binding.RuntimeIdentity != "" {
			identities[binding.RuntimeIdentity] = binding.ID
		}
	}

	clientByID := make(map[string]client.Client, len(clients))
	for _, current := range clients {
		clientByID[current.ID] = current
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
