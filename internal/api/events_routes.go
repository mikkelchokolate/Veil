package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// registerEventsRoutes wires the unified /api/v1/events SSE endpoint (A10).
func (s *managementState) registerEventsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/events", s.handleV1Events)
}

// handleV1Events streams all panel events as SSE. Event types:
//   - traffic: periodic traffic snapshot (every 5s)
//   - apply: apply job state changes (when available)
//   - client: client mutations (created/updated/depleted)
//
// The client can filter by event type via ?types=traffic,apply.
func (s *managementState) handleV1Events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	typesFilter := r.URL.Query().Get("types")
	wantTraffic := typesFilter == "" || containsEventType(typesFilter, "traffic")
	wantApply := typesFilter == "" || containsEventType(typesFilter, "apply")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		writeError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Emit initial state immediately.
	if wantTraffic {
		if !s.emitTrafficEvent(w, rc) {
			return
		}
	}
	if wantApply {
		if !s.emitApplyEvent(w, rc) {
			return
		}
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if wantTraffic && !s.emitTrafficEvent(w, rc) {
				return
			}
			if wantApply && !s.emitApplyEvent(w, rc) {
				return
			}
		}
	}
}

func (s *managementState) emitTrafficEvent(w http.ResponseWriter, rc *http.ResponseController) bool {
	if s.trafficStore == nil {
		return true // no-op, not an error
	}
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

func (s *managementState) emitApplyEvent(w http.ResponseWriter, rc *http.ResponseController) bool {
	s.mu.Lock()
	view := s.applyStateViewLocked()
	s.mu.Unlock()
	data, _ := json.Marshal(map[string]any{
		"at":              time.Now().Unix(),
		"desiredRevision": view.DesiredRevision,
		"appliedRevision": view.AppliedRevision,
		"state":           view.State,
		"activeJobId":     view.ActiveJobID,
		"lastError":       view.LastError,
	})
	fmt.Fprintf(w, "event: apply\ndata: %s\n\n", data)
	rc.Flush()
	return true
}

func containsEventType(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsEventTypeHelper(s, sub))
}

func containsEventTypeHelper(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
