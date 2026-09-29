package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// Presence sources, best-first for the per-client merge.
const (
	presenceSourceStats       = "stats"       // authoritative runtime session table (hysteria2 /online)
	presenceSourceActivity    = "activity"    // counter-increase heuristic (mieru; hysteria2 fallback)
	presenceSourceUnsupported = "unsupported" // no telemetry source exists (naiveproxy, olcRTC, unbound)
	presenceSourceIneligible  = "ineligible"  // render excluded the client (disabled/depleted/expired)
)

const (
	presenceRankStats       = 3
	presenceRankActivity    = 2
	presenceRankUnsupported = 1
)

const (
	// presenceStatsStaleAfter bounds how old an authoritative session table
	// may be before presence stops trusting it. The collector polls every
	// ~30s; beyond ~4 missed polls the answer is stale and the binding falls
	// back to the activity heuristic.
	presenceStatsStaleAfter = 120
	// presenceActivityWindow is the "recent activity" horizon for the
	// counter-increase heuristic: a binding whose counters grew inside the
	// window reads as online.
	presenceActivityWindow = 120
)

// mieruPresenceProviderKey matches the shared-daemon provider key registered
// in buildTrafficProvidersLocked ("mieru:server").
const mieruPresenceProviderKey = "mieru:server"

// presenceItem is the per-client wire shape of GET /api/v1/presence. online
// is tri-state: true/false only when a source could prove it, null when no
// telemetry source can answer — the API never fakes an offline verdict.
type presenceItem struct {
	ClientID     string `json:"clientId"`
	Name         string `json:"name"`
	Online       *bool  `json:"online"`
	Source       string `json:"source"`
	Connections  *int64 `json:"connections,omitempty"`
	LastActiveAt *int64 `json:"lastActiveAt,omitempty"`
}

// bindingPresenceEval is one binding's presence verdict. rank orders sources
// by how authoritative they are for the merge (stats > activity > unsupported).
type bindingPresenceEval struct {
	online       *bool
	source       string
	rank         int
	connections  *int64
	lastActiveAt int64
}

func (s *managementState) registerPresenceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/presence", s.handleV1Presence)
}

// handleV1Presence serves GET /api/v1/presence: one item per client, stable
// by clientId, merging every binding's verdict. The endpoint stays honest
// when telemetry is partial — collector or store absent degrades individual
// fields to null rather than failing or fabricating zeros.
func (s *managementState) handleV1Presence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.clientRepo == nil {
		writeError(w, "client store unavailable", http.StatusServiceUnavailable)
		return
	}
	clients, err := s.clientRepo.AllClients()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bindings, err := s.clientRepo.AllBindings()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	inboundProtocol := make(map[string]string, len(s.inbounds))
	for _, in := range s.inbounds {
		inboundProtocol[in.Name] = in.Protocol
	}
	s.mu.Unlock()

	var presence map[string]client.ProviderPresence
	health := make(map[string]client.ProviderHealth)
	if s.trafficCollector != nil {
		presence = s.trafficCollector.PresenceSnapshot()
		for _, status := range s.trafficCollector.ProviderHealth() {
			health[status.Key] = status
		}
	}
	ids := make([]string, len(clients))
	for i, c := range clients {
		ids[i] = c.ID
	}
	lastActivity := map[string]int64{}
	if s.trafficStore != nil {
		lastActivity, err = s.trafficStore.LastActivityByBinding(ids)
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	bindingsByClient := make(map[string][]client.Binding, len(clients))
	for _, b := range bindings {
		bindingsByClient[b.ClientID] = append(bindingsByClient[b.ClientID], b)
	}
	now := time.Now().Unix()
	items := make([]presenceItem, 0, len(clients))
	for _, c := range clients {
		if !c.RuntimeEligible(now) {
			// The render path already excludes this client (disabled,
			// depleted, or expired): any stats row or counter increase it
			// still carries is residual and must not prove "online"
			// (#1177). Report no verdict rather than a fake "offline" —
			// a session torn down mid-flight is not provably gone.
			items = append(items, presenceItem{ClientID: c.ID, Name: c.Name, Source: presenceSourceIneligible})
			continue
		}
		clientBindings := bindingsByClient[c.ID]
		evals := make([]bindingPresenceEval, 0, len(clientBindings))
		for _, b := range clientBindings {
			if !b.Enabled {
				// Disabled bindings carry no telemetry (the collector's
				// identity map skips them too): they cannot prove presence
				// and must not drag the merge to a fake "offline".
				continue
			}
			evals = append(evals, evalBindingPresence(b, inboundProtocol[b.InboundID], presence, health, lastActivity, now))
		}
		items = append(items, mergeClientPresence(c, evals))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ClientID < items[j].ClientID })
	writeJSON(w, map[string]any{"items": items, "count": len(items)})
}

// evalBindingPresence resolves one binding's presence. hysteria2 trusts the
// runtime's /online session table when it is fresh; every protocol with
// traffic accounting falls back to (or uses) the counter-increase heuristic;
// bindings with no telemetry surface are honestly unsupported.
func evalBindingPresence(b client.Binding, protocol string, presence map[string]client.ProviderPresence, health map[string]client.ProviderHealth, lastActivity map[string]int64, now int64) bindingPresenceEval {
	lastActive := lastActivity[b.ID]
	switch protocol {
	case "hysteria2":
		providerKey := "hysteria2:" + b.InboundID
		if p, ok := presence[providerKey]; ok && p.Online != nil && now-p.ObservedAt <= presenceStatsStaleAfter {
			conns := p.Online[b.ID]
			online := conns > 0
			last := lastActive
			if online && p.ObservedAt > last {
				// A live session IS activity: report the table's own
				// observation time when counters have not ticked recently.
				last = p.ObservedAt
			}
			count := conns
			return bindingPresenceEval{
				online: &online, source: presenceSourceStats, rank: presenceRankStats,
				connections: &count, lastActiveAt: last,
			}
		}
		// The stats listener did not answer (or never reported): counters
		// still flow through the collector, so the activity heuristic is the
		// honest fallback rather than an unknown.
		return activityPresenceEval(lastActive, health[providerKey], now)
	case "mieru":
		return activityPresenceEval(lastActive, health[mieruPresenceProviderKey], now)
	default:
		return bindingPresenceEval{rank: presenceRankUnsupported, source: presenceSourceUnsupported}
	}
}

// activityPresenceEval applies the counter-increase heuristic:
//   - increase inside the window              -> online
//   - last increase known but older           -> offline
//   - never increased, provider observing     -> offline (watched and quiet)
//   - never increased, provider dark/absent   -> unknown (never sampled)
func activityPresenceEval(lastActive int64, health client.ProviderHealth, now int64) bindingPresenceEval {
	eval := bindingPresenceEval{rank: presenceRankActivity, source: presenceSourceActivity, lastActiveAt: lastActive}
	if lastActive > 0 {
		online := now-lastActive <= presenceActivityWindow
		eval.online = &online
		return eval
	}
	if health.State == "healthy" {
		online := false
		eval.online = &online
	}
	return eval
}

// mergeClientPresence folds a client's binding verdicts into one item:
// online when ANY source reports online, otherwise false when any source
// could prove offline, otherwise null. source is the most authoritative
// mechanism among the evals that support the merged verdict.
func mergeClientPresence(c client.Client, evals []bindingPresenceEval) presenceItem {
	item := presenceItem{ClientID: c.ID, Name: c.Name, Source: presenceSourceUnsupported}
	if len(evals) == 0 {
		return item
	}
	sawTrue, sawFalse := false, false
	var connSum int64
	haveConn := false
	var lastActive int64
	for _, e := range evals {
		if e.online != nil {
			if *e.online {
				sawTrue = true
			} else {
				sawFalse = true
			}
		}
		if e.connections != nil {
			connSum += *e.connections
			haveConn = true
		}
		if e.lastActiveAt > lastActive {
			lastActive = e.lastActiveAt
		}
	}
	bestRank := -1
	for _, e := range evals {
		consistent := (sawTrue && e.online != nil && *e.online) ||
			(!sawTrue && sawFalse && e.online != nil && !*e.online) ||
			(!sawTrue && !sawFalse && e.online == nil)
		if consistent && e.rank > bestRank {
			bestRank = e.rank
			item.Source = e.source
		}
	}
	if sawTrue {
		online := true
		item.Online = &online
	} else if sawFalse {
		online := false
		item.Online = &online
	}
	if haveConn {
		item.Connections = &connSum
	}
	if lastActive > 0 {
		item.LastActiveAt = &lastActive
	}
	return item
}
