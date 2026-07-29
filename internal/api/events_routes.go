package api

import (
	"net/http"
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
