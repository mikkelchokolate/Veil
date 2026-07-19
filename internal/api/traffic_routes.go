package api

import (
	"encoding/json"
	"fmt"
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
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	up, down, err := s.trafficStore.TotalsForClient(clientID)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cl, _ := s.clientService.Get(clientID)
	used := up + down
	var remaining *int64
	if cl.QuotaBytes != nil {
		rem := *cl.QuotaBytes - used
		remaining = &rem
	}
	writeJSON(w, map[string]any{
		"clientId":       clientID,
		"uploadBytes":    up,
		"downloadBytes":  down,
		"usedBytes":      used,
		"quotaBytes":     cl.QuotaBytes,
		"remainingBytes": remaining,
		"depleted":       cl.QuotaBytes != nil && used >= *cl.QuotaBytes,
		"collectedAt":    time.Now().Unix(),
	})
}

func (s *managementState) handleV1TrafficHistory(w http.ResponseWriter, r *http.Request, clientID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	from := parseInt64Default(r.URL.Query().Get("from"), 0)
	to := parseInt64Default(r.URL.Query().Get("to"), time.Now().Unix())
	limit := int(parseInt64Default(r.URL.Query().Get("limit"), 500))
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
	if s.trafficCollector != nil {
		providerCount = s.trafficCollector.ProviderCount()
		if providerCount > 0 {
			state = "collecting"
		}
	}
	resp := map[string]any{
		"state":         state,
		"providerCount": providerCount,
	}
	if s.trafficStore != nil {
		var up, down int64
		clients, _, err := s.clientService.List(client.ListFilter{PageSize: 1000})
		if err == nil {
			for _, c := range clients {
				u, d, _ := s.trafficStore.TotalsForClient(c.ID)
				up += u
				down += d
			}
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
	limit := int(parseInt64Default(r.URL.Query().Get("limit"), 10))
	clients, _, err := s.clientService.List(client.ListFilter{PageSize: 1000})
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
	entries := make([]entry, 0, len(clients))
	for _, c := range clients {
		up, down, _ := s.trafficStore.TotalsForClient(c.ID)
		if up+down == 0 {
			continue
		}
		entries = append(entries, entry{ClientID: c.ID, Name: c.Name, UploadBytes: up, DownloadBytes: down, UsedBytes: up + down})
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
	if s.trafficStore == nil {
		writeError(w, "traffic store unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Use ResponseController to reach through middleware-wrapped writers.
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		writeError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// Emit one immediate snapshot, then on each tick.
	emit := func() bool {
		clients, _, err := s.clientService.List(client.ListFilter{PageSize: 1000})
		if err != nil {
			return false
		}
		snap := map[string]any{"at": time.Now().Unix(), "clients": map[string]any{}}
		for _, c := range clients {
			up, down, _ := s.trafficStore.TotalsForClient(c.ID)
			snap["clients"].(map[string]any)[c.ID] = map[string]int64{"upload": up, "download": down}
		}
		data, _ := json.Marshal(snap)
		fmt.Fprintf(w, "event: traffic\ndata: %s\n\n", data)
		rc.Flush()
		return true
	}
	if !emit() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !emit() {
				return
			}
		}
	}
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
