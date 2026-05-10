package api

import (
	"net/http"

	"github.com/veil-panel/veil/internal/observability"
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
	ServiceActionRoutes{}.Register(mux)
	state.register(mux)
	PanelRoutes{Info: info, BasePath: basePath}.Register(mux)
	DiagnosticToolRoutes{}.Register(mux)
	StatusRoutes{Info: info}.Register(mux)
	ProfilePreviewRoutes{}.Register(mux)
	LogRoutes{}.Register(mux)

	var handler http.Handler = mux
	if basePath != "/" {
		handler = stripBasePathMiddleware(basePath, mux)
	}
	rateLimited := rateLimitMiddleware(metrics, handler)
	authenticated := authMiddleware(info.AuthToken, rateLimited)
	secured := securityHeadersMiddleware(authenticated)
	return metrics.MetricsMiddleware(secured), state
}
