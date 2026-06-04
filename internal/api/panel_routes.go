package api

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"time"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/panel"
)

type PanelRoutes struct {
	Info     ServerInfo
	BasePath string
	State    *managementState
}

func (routes PanelRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", routes.handlePanel)
	mux.HandleFunc("/favicon.ico", routes.handleFavicon)
	mux.HandleFunc("/healthz", routes.handleHealth)
	mux.HandleFunc("/api/version", routes.handleVersion)
	mux.HandleFunc("/api/version/update", routes.handleUpdateVersion)
}

func (routes PanelRoutes) handleFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#ffe6cb"><path d="M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm-9 12H5v-2h6v2zm4-4H5v-2h10v2zm4-4H5V6h14v2z"/></svg>`))
	}
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
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://api.qrserver.com; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Origin-Agent-Cluster", "?1")
	if r.Method == http.MethodGet {
		// Check session; if no valid session, show login page.
		csrfToken := ""
		if routes.State != nil {
			var authenticated bool
			cookie, err := r.Cookie("veil_session")
			if err == nil {
				if sess, ok := globalSessions.Get(cookie.Value); ok {
					authenticated = true
					csrfToken = sess.CSRFToken
				}
			}
			if !authenticated {
				// Also allow if static auth token is set (old API clients bypass session)
				// or if there are no users at all (dev mode)
				routes.State.mu.Lock()
				noUsers := len(routes.State.users) == 0
				routes.State.mu.Unlock()
				if routes.Info.PublicListen && noUsers {
					writeError(w, "first-run admin setup is required before public Panel access; run `veil admin reset` or `veil admin set --username admin --password <password>`", http.StatusServiceUnavailable)
					return
				}
				if !noUsers {
					_, _ = w.Write([]byte(panel.LoginHTML(routes.BasePath)))
					return
				}
			}
		}
		_, _ = w.Write([]byte(panelHTML(routes.BasePath, csrfToken)))
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

func panelHTML(basePath string, csrfToken string) string {
	return panel.NewRenderer(panel.NewSliceCatalog(NewManagedRuntimeCatalog().Runtimes()).RenderSlots()).HTML(basePath, csrfToken)
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

func (routes PanelRoutes) handleUpdateVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := validateEmptyJSONBody(r); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	setJSONHeaders(w)

	var logBuf bytes.Buffer

	deps := updateflow.WorkflowDependencies{
		FetchRelease: func() (*updateflow.Release, error) {
			catalog := updateflow.NewReleaseCatalog("mikkelchokolate", "Veil")
			catalog.HTTPClient = &http.Client{Timeout: 30 * time.Second}
			return catalog.Latest()
		},
		DownloadAsset: func(url string) ([]byte, error) {
			updateflow.HTTPClient = &http.Client{Timeout: 30 * time.Second}
			return updateflow.DownloadAsset(url)
		},
	}

	opts := updateflow.WorkflowOptions{
		CurrentVersion: routes.Info.Version,
		Yes:            true,
		DryRun:         false,
		Force:          false,
		Restart:        false,
		Staged:         true,
	}

	err := updateflow.RunWorkflow(opts, &logBuf, deps)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"log":     logBuf.String(),
			"message": err.Error(),
		})
		return
	}

	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("systemctl", "restart", "veil.service").Run()
	}()

	writeJSON(w, map[string]any{
		"success": true,
		"log":     logBuf.String(),
		"message": "Update staged successfully. Restarting panel service...",
	})
}
