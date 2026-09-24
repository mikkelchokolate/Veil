package api

import (
	"io"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/clientaddr"
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
	state := newManagementStateProduction(info)
	metrics := observability.NewMetricsCollector()
	state.metrics = metrics
	c.registerMux(mux, info, state, metrics, basePath)

	state.idempotency = newIdempotencyStore(state.db)
	if err := state.idempotency.setReplayCipher(state.cipher); err != nil {
		_ = state.idempotency.Close()
	}
	restoreGuarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if info.RequirePrivilegedHelper {
			switch r.URL.Path {
			case "/api/tls", "/api/network", "/api/connections", "/api/processes", "/api/disk", "/api/runtime/observation":
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
	gated := clientRequestGateMiddleware(state, idempotent)
	trustedProxies := info.TrustedProxyCIDRs
	if len(trustedProxies) == 0 {
		trustedProxies = clientaddr.DefaultTrustedProxyCIDRs(info.PanelAccess)
	}
	rateLimited, limiter := newRateLimitMiddleware(metrics, trustedProxies, gated)
	state.httpRateLimiter = limiter
	authenticated := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             info.AuthToken,
		ProtectHealthz:    info.PublicListen,
		ProtectMetrics:    info.MetricsAuthRequired || info.PublicListen,
		AllowDevAnonymous: !info.PublicListen,
		AllowSetup:        info.SetupAllowed,
	}, rateLimited)
	// Strip WebBasePath before auth. capabilityForEndpoint treats anything
	// outside /api and /s as a public SPA route; classifying /<base>/api/*
	// before the strip made the whole management API anonymous.
	// Metrics observe the post-strip path: recording the outer request would
	// label every mounted route /<base>/api/* as /{unmatched} (issue #979).
	var handler http.Handler = metrics.MetricsMiddleware(authenticated)
	if basePath != "/" {
		handler = stripBasePathMiddleware(basePath, handler)
	}
	secured := securityHeadersMiddleware(handler)
	healthAware := auditHealthMiddleware(state, secured)
	return requestIDMiddleware(degradedStateMiddleware(state, healthAware)), state
}

// registerMux wires every route registration onto mux in one place so
// contract tests can interrogate the exact live registration set through
// mux.Handler instead of a hand-maintained route list (issue #923).
func (RouterComposition) registerMux(mux *http.ServeMux, info ServerInfo, state *managementState, metrics *observability.MetricsCollector, basePath string) {
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics.ServeHTTP(w, r)
		if r.Method == http.MethodGet && state.trafficCollector != nil {
			_, _ = io.WriteString(w, state.trafficCollector.PrometheusMetrics())
		}
	})
	RuntimeRoutes{State: state}.Register(mux)
	mux.HandleFunc("/api/services/", state.handleServiceActionRoute)
	state.register(mux)
	panelRoutes := PanelRoutes{Info: info, BasePath: basePath, State: state}
	panelRoutes.Register(mux)
	DiagnosticToolRoutes{}.Register(mux)
	StatusRoutes{Info: info, State: state}.Register(mux)
	HealthRoutes{State: state}.Register(mux)
	ProfilePreviewRoutes{}.Register(mux)
	LogRoutes{State: state}.Register(mux)
}
