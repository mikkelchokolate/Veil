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
	s.serveSharedSSE(w, r, parseSSETypes(r.URL.Query().Get("types")))
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
