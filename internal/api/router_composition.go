package api

import (
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
	mux.HandleFunc("/metrics", metrics.ServeHTTP)
	RuntimeRoutes{}.Register(mux)
	mux.HandleFunc("/api/services/", state.handleServiceActionRoute)
	state.register(mux)
	PanelRoutes{Info: info, BasePath: basePath, State: state}.Register(mux)
	DiagnosticToolRoutes{}.Register(mux)
	StatusRoutes{Info: info, State: state}.Register(mux)
	ProfilePreviewRoutes{}.Register(mux)
	LogRoutes{State: state}.Register(mux)

	var handler http.Handler = mux
	if basePath != "/" {
		handler = stripBasePathMiddleware(basePath, mux)
	}
	rateLimited := rateLimitMiddleware(metrics, info.TrustedProxyCIDRs, handler)
	authenticated := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             info.AuthToken,
		ProtectHealthz:    info.PublicListen,
		ProtectMetrics:    info.MetricsAuthRequired,
		AllowDevAnonymous: !info.PublicListen,
		AllowSetup:        info.SetupAllowed,
	}, rateLimited)
	secured := securityHeadersMiddleware(authenticated)
	return metrics.MetricsMiddleware(secured), state
}
