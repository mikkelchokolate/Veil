package api

import (
	"io"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/observability"
	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

type RouterComposition struct {
	Info ServerInfo
}

func NewRouterComposition(info ServerInfo) RouterComposition {
	return RouterComposition{Info: info}
}

func (c RouterComposition) Build() (http.Handler, Reloader) {
	info := c.Info
	basePath, err := webbasepath.Normalize(info.WebBasePath)
	if err != nil {
		// ServerInfo may be built directly in tests or embedded integrations rather
		// than through the validated serve configuration. Fail closed to root so an
		// unsafe value cannot be inserted into routes or rendered JavaScript.
		basePath = "/"
	}
	info.WebBasePath = basePath

	mux := http.NewServeMux()
	state := newManagementState(info)
	metrics := observability.NewMetricsCollector()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics.ServeHTTP(w, r)
		if r.Method == http.MethodGet && state.trafficCollector != nil {
			_, _ = io.WriteString(w, state.trafficCollector.PrometheusMetrics())
		}
	})
	RuntimeRoutes{}.Register(mux)
	mux.HandleFunc("/api/services/", state.handleServiceActionRoute)
	state.register(mux)
	panelRoutes := PanelRoutes{Info: info, BasePath: basePath, State: state}
	panelRoutes.Register(mux)
	DiagnosticToolRoutes{}.Register(mux)
	StatusRoutes{Info: info, State: state}.Register(mux)
	HealthRoutes{State: state}.Register(mux)
	ProfilePreviewRoutes{}.Register(mux)
	LogRoutes{State: state}.Register(mux)

	state.idempotency = newIdempotencyStore(state.db)
	if err := state.idempotency.setReplayCipher(state.cipher); err != nil {
		_ = state.idempotency.Close()
	}
	restoreGuarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if info.RequirePrivilegedHelper {
			switch r.URL.Path {
			case "/api/system", "/api/tls", "/api/network", "/api/connections", "/api/processes", "/api/disk", "/api/runtime/observation":
				writeError(w, "diagnostic requires bounded root-helper support", http.StatusServiceUnavailable)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			state.mu.Lock()
			restoring := state.clientSubsystemStopping
			state.mu.Unlock()
			if restoring {
				w.Header().Set("Retry-After", "5")
				writeError(w, "management mutation is locked while restore is in progress", http.StatusLocked)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
	idempotent := state.idempotency.Middleware(restoreGuarded)
	var handler http.Handler = clientRequestGateMiddleware(state, idempotent)
	if basePath != "/" {
		handler = stripBasePathMiddleware(basePath, handler)
	}
	rateLimited, limiter := newRateLimitMiddleware(metrics, info.TrustedProxyCIDRs, handler)
	state.httpRateLimiter = limiter
	authenticated := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             info.AuthToken,
		ProtectHealthz:    info.PublicListen,
		ProtectMetrics:    info.MetricsAuthRequired || info.PublicListen,
		AllowDevAnonymous: !info.PublicListen,
		AllowSetup:        info.SetupAllowed,
	}, rateLimited)
	secured := securityHeadersMiddleware(authenticated)
	healthAware := auditHealthMiddleware(state, metrics.MetricsMiddleware(secured))
	return requestIDMiddleware(degradedStateMiddleware(state, healthAware)), state
}
