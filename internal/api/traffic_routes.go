package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// registerTrafficRoutes wires /api/v1/traffic read endpoints and the SSE
// live-traffic stream.
func (s *managementState) registerTrafficRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/traffic/top", s.handleV1TrafficTop)
	mux.HandleFunc("/api/v1/traffic/stream", s.handleV1TrafficStream)
	mux.HandleFunc("/api/v1/traffic/summary", s.handleV1TrafficSummary)
	mux.HandleFunc("/api/v1/traffic/", s.handleV1TrafficClient)
}

// handleV1TrafficClient serves per-client traffic totals and history.
// Paths:
//
//	/api/v1/traffic/{clientId}           -> totals
//	/api/v1/traffic/{clientId}/history   -> bucketed history
func (s *managementState) handleV1TrafficClient(w http.ResponseWriter, r *http.Request) {
	if s.trafficStore == nil {
		writeError(w, "traffic store unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/traffic/")
	parts := splitNonEmpty(rest, "/")
	if len(parts) == 0 {
		writeNotFound(w)
		return
	}
	clientID := parts[0]
	if _, err := s.clientService.Get(clientID); err != nil {
		s.writeV1ClientError(w, err)
		return
	}
	if len(parts) == 2 && parts[1] == "history" {
		s.handleV1TrafficHistory(w, r, clientID)
		return
	}
	if len(parts) != 1 {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	snap, err := s.trafficStore.SnapshotForClient(clientID)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cl, _ := s.clientService.Get(clientID)
	used := snap.UploadBytes + snap.DownloadBytes
	var remaining *int64
	if cl.QuotaBytes != nil {
		rem := *cl.QuotaBytes - used
		remaining = &rem
	}
	resp := map[string]any{
		"clientId":       clientID,
		"uploadBytes":    snap.UploadBytes,
		"downloadBytes":  snap.DownloadBytes,
		"usedBytes":      used,
		"quotaBytes":     cl.QuotaBytes,
		"remainingBytes": remaining,
		"depleted":       cl.QuotaBytes != nil && used >= *cl.QuotaBytes,
		"state":          clientTrafficReportState(s, cl, snap),
	}
	if snap.LastObservedAt > 0 {
		resp["collectedAt"] = snap.LastObservedAt
	} else {
		resp["collectedAt"] = nil
	}
	writeJSON(w, resp)
}

func clientTrafficReportState(s *managementState, view client.View, snap client.ClientTrafficSnapshot) string {
	accounting := false
	for _, binding := range view.Bindings {
		if binding.Capability != nil && binding.Capability.TrafficAccounting {
			accounting = true
			break
		}
	}
	degraded := false
	if s.trafficCollector != nil {
		for _, health := range s.trafficCollector.ProviderHealth() {
			if health.State == "degraded" {
				degraded = true
				break
			}
		}
	}
	if snap.LastObservedAt == 0 {
		if accounting {
			return "pending"
		}
		return "unsupported"
	}
	if degraded {
		return "stale"
	}
	return "healthy"
}

func (s *managementState) handleV1TrafficHistory(w http.ResponseWriter, r *http.Request, clientID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	from := parseInt64Default(r.URL.Query().Get("from"), 0)
	to := parseInt64Default(r.URL.Query().Get("to"), time.Now().Unix())
	limit := parseLimitParam(r.URL.Query().Get("limit"), 500, 5000)
	rows, err := s.trafficStore.HistoryForClient(clientID, from, to, limit)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"items": rows, "count": len(rows)})
}

// handleV1TrafficSummary returns aggregate traffic totals over the whole set
// plus the honest telemetry provider state so the UI never renders fake zeros
// as real data.
func (s *managementState) handleV1TrafficSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	// Telemetry state: honest about whether any runtime is feeding counters.
	state := "unsupported"
	providerCount := 0
	var providers []client.ProviderHealth
	if s.trafficCollector != nil {
		providerCount = s.trafficCollector.ProviderCount()
		providers = s.trafficCollector.ProviderHealth()
		if providerCount > 0 {
			state = "healthy"
			for _, provider := range providers {
				if provider.State == "degraded" {
					state = "degraded"
					break
				}
				if provider.State != "healthy" || provider.LastSuccessfulObservationAt == 0 {
					state = "pending"
				}
			}
		}
	}
	resp := map[string]any{
		"state":         state,
		"providerCount": providerCount,
		"providers":     providers,
	}
	if s.trafficStore != nil {
		up, down, err := s.trafficStore.AggregateTotals()
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp["uploadBytes"] = up
		resp["downloadBytes"] = down
		resp["usedBytes"] = up + down
	}
	writeJSON(w, resp)
}

// handleV1TrafficTop returns clients ranked by total usage (top talkers).
func (s *managementState) handleV1TrafficTop(w http.ResponseWriter, r *http.Request) {
	if s.trafficStore == nil {
		writeError(w, "traffic store unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	limit := parseLimitParam(r.URL.Query().Get("limit"), 10, 500)
	if s.clientRepo == nil {
		writeError(w, "client store unavailable", http.StatusServiceUnavailable)
		return
	}
	clients, err := s.clientRepo.AllClients()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type entry struct {
		ClientID      string `json:"clientId"`
		Name          string `json:"name"`
		UploadBytes   int64  `json:"uploadBytes"`
		DownloadBytes int64  `json:"downloadBytes"`
		UsedBytes     int64  `json:"usedBytes"`
	}
	ids := make([]string, len(clients))
	for i, current := range clients {
		ids[i] = current.ID
	}
	totals, err := s.trafficStore.TotalsForClients(ids)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries := make([]entry, 0, len(clients))
	for _, current := range clients {
		pair := totals[current.ID]
		if pair[0]+pair[1] == 0 {
			continue
		}
		entries = append(entries, entry{ClientID: current.ID, Name: current.Name, UploadBytes: pair[0], DownloadBytes: pair[1], UsedBytes: pair[0] + pair[1]})
	}
	// Sort by used desc (simple insertion sort; N is small).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].UsedBytes > entries[j-1].UsedBytes; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	writeJSON(w, map[string]any{"items": entries})
}

// handleV1TrafficStream streams periodic traffic snapshots as SSE events.
// Clients (panel dashboards) subscribe for live counters without polling.
func (s *managementState) handleV1TrafficStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.serveSharedSSE(w, r, map[string]bool{"traffic": true})
}

func parseInt64Default(v string, def int64) int64 {
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// parseLimitParam parses an int64 query value and narrows it to int only
// inside a branch guarded by a COMPILE-TIME-CONSTANT bound: a huge ?limit=
// (e.g. 2^60) can never reach the conversion, and non-positive values are
// meaningless for LIMIT clauses. (CodeQL go/incorrect-integer-conversion
// proves the guard only against constants — parameter-derived bounds are not
// accepted.)
func parseLimitParam(v string, def, max int) int {
	const ceiling = 10000 // fits int32 with orders of magnitude to spare
	n := parseInt64Default(v, int64(def))
	if n >= 1 && n <= ceiling {
		m := int(n)
		if m > max {
			return max
		}
		return m
	}
	if n > ceiling {
		return max
	}
	return def
}

// splitNonEmpty splits a path on sep, dropping empty segments.
func splitNonEmpty(s, sep string) []string {
	raw := strings.Split(s, sep)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
