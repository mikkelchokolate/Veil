package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	webui "github.com/mikkelchokolate/Veil/web"
)

// spaHandler serves the embedded React SPA (B11).
//
// The SPA is served at "/" and under the secret WebBasePath. It is a purely
// static bundle: authentication, first-run setup, and RBAC are all handled
// client-side against /api/*. The public token subscription endpoint
// (/s/{token}) is routed elsewhere and never reaches this handler.
type spaHandler struct {
	dist     fs.FS
	basePath string
}

func newSPAHandler(basePath string) (*spaHandler, error) {
	sub, err := fs.Sub(webui.Dist, "dist")
	if err != nil {
		return nil, err
	}
	return &spaHandler{dist: sub, basePath: basePath}, nil
}

// Register mounts the SPA. Asset URLs are content-hashed (immutable); the SPA
// shell and client routes are no-store. "/assets/" serves files directly; any
// other GET that is not an API/subscription path serves the SPA shell so
// client-side routing works (history-API fallback).
func (h *spaHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/assets/", h.serveAsset)
}

// serveAsset serves a hashed static asset from the embedded bundle.
func (h *spaHandler) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	serveSPAFile(w, r, h.dist, name, "public, max-age=31536000, immutable")
}

// serveIndex serves the SPA shell with the <base href> rewritten to the panel
// WebBasePath so relative asset URLs resolve under "/<secret>".
func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	data, err := fs.ReadFile(h.dist, "index.html")
	if err != nil {
		writeNotFound(w)
		return
	}
	html := string(data)
	base := h.basePath
	if base == "" {
		base = "/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	html = strings.Replace(html, `<base href="/" />`, `<base href="`+base+`" />`, 1)
	setSPAHeaders(w, "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// matches reports whether a request path should render the SPA shell. API,
// metrics, health, subscription, and asset paths are excluded.
func (h *spaHandler) matches(p string) bool {
	if p == "/" {
		return true
	}
	for _, prefix := range []string{"/api/", "/s/", "/assets/", "/metrics", "/healthz", "/favicon.ico"} {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}
	// Client-side routes have no file extension.
	return !strings.Contains(path.Base(p), ".")
}

func serveSPAFile(w http.ResponseWriter, r *http.Request, dist fs.FS, name, cache string) {
	data, err := fs.ReadFile(dist, name)
	if err != nil {
		writeNotFound(w)
		return
	}
	setSPAHeaders(w, cache)
	base := path.Base(name)
	switch {
	case strings.HasSuffix(base, ".js"), strings.HasSuffix(base, ".mjs"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(base, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(base, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(base, ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
	case strings.HasSuffix(base, ".map"):
		w.Header().Set("Content-Type", "application/json")
	}
	_, _ = w.Write(data)
}

func setSPAHeaders(w http.ResponseWriter, cache string) {
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if cache == "no-store" {
		w.Header().Set("Pragma", "no-cache")
	}
}
