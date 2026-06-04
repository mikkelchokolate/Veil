package api

import (
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/observability"
)

type RouterComposition struct {
	Info ServerInfo
}

func NewRouterComposition(info ServerInfo) RouterComposition {
	return RouterComposition{Info: info}
}

func (c RouterComposition) Build() (http.Handler, Reloader) {
	info := c.Info
	mux := http.NewServeMux()
	state := newManagementState(info)
	metrics := observability.NewMetricsCollector()
	basePath := info.WebBasePath
	if basePath == "" {
		basePath = "/"
	}
	mux.HandleFunc("/metrics", metrics.ServeHTTP)
	RuntimeRoutes{}.Register(mux)
	mux.HandleFunc("/api/services/", state.handleServiceActionRoute)
	state.register(mux)
	PanelRoutes{Info: info, BasePath: basePath, State: state}.Register(mux)
	DiagnosticToolRoutes{}.Register(mux)
	StatusRoutes{Info: info}.Register(mux)
	ProfilePreviewRoutes{}.Register(mux)
	LogRoutes{}.Register(mux)

	var handler http.Handler = mux
	if basePath != "/" {
		handler = stripBasePathMiddleware(basePath, mux)
	}
	rateLimited := rateLimitMiddleware(metrics, handler)
	authenticated := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             info.AuthToken,
		ProtectHealthz:    info.PublicListen,
		ProtectMetrics:    info.MetricsAuthRequired,
		AllowDevAnonymous: !info.PublicListen,
	}, rateLimited)
	secured := securityHeadersMiddleware(authenticated)
	return metrics.MetricsMiddleware(secured), state
}
