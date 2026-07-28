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
