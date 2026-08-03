package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/mikkelchokolate/Veil/internal/panel"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

var (
	legacyStyleTag      = regexp.MustCompile(`<style(\s|>)`)
	legacyScriptTag     = regexp.MustCompile(`<script(\s|>)`)
	legacyRemoteFontTag = regexp.MustCompile(`<link[^>]+(?:fonts\.googleapis\.com|fonts\.gstatic\.com)[^>]*>`)
)

func secureLegacyPanelHTML(body string) (string, string, error) {
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	secured := legacyRemoteFontTag.ReplaceAllString(body, "")
	secured = legacyStyleTag.ReplaceAllString(secured, `<style nonce="`+nonce+`"$1`)
	secured = legacyScriptTag.ReplaceAllString(secured, `<script nonce="`+nonce+`"$1`)
	return secured, fmt.Sprintf("default-src 'self'; img-src 'self' data: blob:; script-src 'self' 'nonce-%s'; style-src 'self' 'nonce-%s'; font-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'", nonce, nonce), nil
}

type PanelRoutes struct {
	Info     ServerInfo
	BasePath string
	State    *managementState
	spa      *spaHandler
}

func (routes *PanelRoutes) Register(mux *http.ServeMux) {
	if spa, err := newSPAHandler(routes.BasePath); err == nil {
		routes.spa = spa
		spa.Register(mux)
	}
	mux.HandleFunc("/", routes.handlePanel)
	mux.HandleFunc("/favicon.ico", routes.handleFavicon)
	mux.HandleFunc("/healthz", routes.handleHealth)
	mux.HandleFunc("/api/version", routes.handleVersion)
	mux.HandleFunc("/api/version/update", routes.handleUpdateVersion)
	mux.HandleFunc("/api/version/update/jobs/", routes.handlePanelUpdateJob)
	if routes.State != nil && routes.State.db != nil {
		routes.State.reconcilePanelUpdateJobs(routes.Info.Version)
	}
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}

	// SPA client-side routes (e.g. /clients, /apply) render the shell via the
	// history-API fallback. API/asset/subscription/metrics paths are handled by
	// their own mux registrations and never reach here.
	if r.URL.Path != "/" {
		if routes.spa != nil && routes.spa.matches(r.URL.Path) {
			routes.spa.serveIndex(w, r)
			return
		}
		writeNotFound(w)
		return
	}

	// Fail-closed guard (preserved): on a public listener with no users yet the
	// panel must NOT render any HTML — first-run admin setup is CLI-only there.
	if routes.State != nil {
		routes.State.mu.Lock()
		noUsers := len(routes.State.users) == 0
		routes.State.mu.Unlock()
		if routes.Info.PublicListen && noUsers {
			writeError(w, "first-run admin setup is required before public Panel access; run `veil admin reset` or `veil admin set --username admin --password <password>`", http.StatusServiceUnavailable)
			return
		}
	}

	// The new React SPA is the primary UI (B11): served at "/", it handles
	// login, first-run setup, and RBAC client-side against the API.
	if routes.spa != nil {
		routes.spa.serveIndex(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Origin-Agent-Cluster", "?1")
	writeLegacyHTML := func(body string) bool {
		secured, csp, err := secureLegacyPanelHTML(body)
		if err != nil {
			writeError(w, "panel security policy unavailable", http.StatusInternalServerError)
			return false
		}
		w.Header().Set("Content-Security-Policy", csp)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(secured))
		}
		return true
	}
	if r.Method == http.MethodHead {
		writeLegacyHTML("")
		return
	}
	if r.Method == http.MethodGet {
		// Check session; if no valid session, show login page.
		csrfToken := ""
		locale := panel.ResolveLocale("", r)
		if routes.State != nil {
			var authenticated bool
			cookie, err := r.Cookie("veil_session")
			if err == nil {
				if session, ok := routes.State.sessionRegistry().Get(cookie.Value); ok {
					authenticated = true
					locale = panel.ResolveLocale(routes.State.storedUserLocale(session.Username), r)
					csrfToken, _, _ = routes.State.sessionRegistry().EnsureCSRF(cookie.Value)
				}
			}
			if !authenticated {
				routes.State.mu.Lock()
				noUsers := len(routes.State.users) == 0
				setupRequired := routes.State.setupAllowed && !routes.State.setup.Completed && noUsers
				routes.State.mu.Unlock()
				if setupRequired {
					writeLegacyHTML(panel.ReliableSetupHTML(routes.BasePath, locale))
					return
				}
				if routes.Info.PublicListen && noUsers {
					writeError(w, "first-run admin setup is required before public Panel access; run `veil admin reset` or `veil admin set --username admin --password <password>`", http.StatusServiceUnavailable)
					return
				}
				if !noUsers {
					writeLegacyHTML(panel.ReliableLoginHTML(routes.BasePath, locale))
					return
				}
			}
		}
		writeLegacyHTML(panelHTMLForCatalog(routes.BasePath, csrfToken, locale, NewVisibleManagedRuntimeCatalogForState(routes.State)))
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
					"error":  "management state unavailable",
				})
				return
			}
		}
		writeJSON(w, map[string]string{"status": "ok"})
	}
}

func panelHTMLForCatalog(basePath string, csrfToken string, locale string, catalog ManagedRuntimeCatalog) string {
	return panel.AuthenticationExpiryReliableHTML(panel.StorageReliableHTML(panel.NewRenderer(panel.NewSliceCatalog(catalog.Runtimes()).RenderSlots()).HTML(basePath, panel.EscapeJavaScriptString(csrfToken), locale)))
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

	if routes.State == nil || routes.State.privileged == nil {
		writePrivilegedHelperUnavailable(w)
		return
	}
	if !routes.State.beginPanelUpdate(w) {
		return
	}
	releaseLocks := true
	defer func() {
		if releaseLocks {
			routes.State.endPanelUpdate()
		}
	}()
	if routes.State.updateStager == nil {
		writeError(w, "panel update staging is unavailable", http.StatusServiceUnavailable)
		return
	}
	version, err := routes.State.updateStager(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusBadGateway)
		return
	}
	updateJob, err := routes.State.createPanelUpdateJob(version)
	if err != nil {
		writeError(w, "create durable update job", http.StatusInternalServerError)
		return
	}
	result, applyJob, err := routes.State.installPanelUpdate(r.Context(), version)
	if err != nil {
		routes.State.updatePanelUpdateJob(updateJob.ID, "failed", applyJob.ID, "", err)
		writePrivilegedError(w, err)
		return
	}
	if !result.Installed {
		writePrivilegedError(w, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper did not install the staged update",
		})
		return
	}
	routes.State.updatePanelUpdateJob(updateJob.ID, "restart_pending", applyJob.ID, "", nil)
	releaseLocks = false
	routes.State.updateWG.Add(1)
	go func() {
		defer routes.State.updateWG.Done()
		routes.State.restartPanelForUpdate(updateJob.ID)
	}()

	writeJSONStatus(w, http.StatusAccepted, map[string]any{
		"jobId":     updateJob.ID,
		"status":    "restart_pending",
		"staged":    result.Staged,
		"installed": result.Installed,
		"version":   result.Version,
		"message":   "Update installed; durable restart verification is pending.",
	})
}

func writePrivilegedHelperUnavailable(w http.ResponseWriter) {
	const message = "privileged helper is unavailable; repair the native install with `sudo /usr/local/bin/veil repair --yes`, then run `sudo systemctl enable --now veil-helper.socket` and `sudo systemctl restart veil.service`. If you are upgrading from a pre-helper release, rerun the curl installer with `install.sh --force`."
	writeErrorEnvelope(w, string(privileged.ErrorOperationFailed), message, http.StatusServiceUnavailable)
}
