package api

import (
	"net/http"
	"os"
)

type PanelRoutes struct {
	Info     ServerInfo
	BasePath string
}

func (PanelRoutes) Paths() []string {
	return []string{"/", "/healthz", "/api/version"}
}

func (routes PanelRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", routes.handlePanel)
	mux.HandleFunc("/healthz", routes.handleHealth)
	mux.HandleFunc("/api/version", routes.handleVersion)
}

func (routes PanelRoutes) handlePanel(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Origin-Agent-Cluster", "?1")
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(panelHTML(routes.BasePath)))
	}
}

func (routes PanelRoutes) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet {
		if routes.Info.StatePath != "" {
			if _, err := os.Stat(routes.Info.StatePath); err != nil {
				writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{
					"status": "unhealthy",
					"error":  err.Error(),
				})
				return
			}
		}
		writeJSON(w, map[string]string{"status": "ok"})
	}
}

func (routes PanelRoutes) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]string{
			"version": routes.Info.Version,
			"runtime": runtimeInfo(),
			"name":    "Veil",
		})
	}
}
