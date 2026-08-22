package api

import (
	"net/http"
	"strings"
)

func clientRequestGateMiddleware(state *managementState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Streaming requests are long lived. The shared broadcaster acquires this
		// gate only while it builds each bounded snapshot.
		if r.URL.Path == "/api/v1/events" || r.URL.Path == "/api/v1/traffic/stream" ||
			strings.HasPrefix(r.URL.Path, "/api/backup-restore-jobs/") {
			next.ServeHTTP(w, r)
			return
		}
		state.clientRequestMu.RLock()
		defer state.clientRequestMu.RUnlock()
		next.ServeHTTP(w, r)
	})
}

func degradedStateMiddleware(state *managementState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			state.mu.Lock()
			storageUnavailable := state.storageDegradedErr != nil || (state.requireApplyTracking && state.db == nil)
			stateUnavailable := state.startupStateLoadFailed
			privilegedUnavailable := state.startupPrivilegedFailure
			state.mu.Unlock()
			if privilegedUnavailable {
				writePrivilegedHelperUnavailable(w)
				return
			}
			if storageUnavailable || stateUnavailable {
				code := "state_unavailable"
				component := "state_store"
				if storageUnavailable {
					code = "storage_unavailable"
					component = "sqlite"
				}
				setJSONHeaders(w)
				w.WriteHeader(http.StatusServiceUnavailable)
				writeJSON(w, map[string]any{
					"error":     map[string]string{"code": code, "message": "management mutations are unavailable while persistent state is degraded"},
					"component": component,
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
