package api

import (
	"compress/gzip"
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
	writeSPABody(w, r, []byte(html), true)
}

// matches reports whether a request path should render the SPA shell. API,
// metrics, health, subscription, and asset paths are excluded.
func (h *spaHandler) matches(p string) bool {
	if p == "/" {
		return true
	}
	for _, prefix := range []string{"/api/", "/s/", "/assets/", "/metrics", "/healthz", "/favicon.ico", "/favicon.svg"} {
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
	compress := false
	switch {
	case strings.HasSuffix(base, ".js"), strings.HasSuffix(base, ".mjs"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		compress = true
	case strings.HasSuffix(base, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		compress = true
	case strings.HasSuffix(base, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
		compress = true
	case strings.HasSuffix(base, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		compress = true
	case strings.HasSuffix(base, ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
	case strings.HasSuffix(base, ".jpg"), strings.HasSuffix(base, ".jpeg"):
		w.Header().Set("Content-Type", "image/jpeg")
	case strings.HasSuffix(base, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(base, ".webp"):
		w.Header().Set("Content-Type", "image/webp")
	case strings.HasSuffix(base, ".map"):
		w.Header().Set("Content-Type", "application/json")
		compress = true
	}
	writeSPABody(w, r, data, compress)
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func writeSPABody(w http.ResponseWriter, r *http.Request, data []byte, compress bool) {
	if r.Method == http.MethodHead {
		return
	}
	if compress && acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write(data)
		_ = gz.Close()
		return
	}
	_, _ = w.Write(data)
}

func setSPAHeaders(w http.ResponseWriter, cache string) {
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// SPA CSP (B12): no unsafe-inline scripts/styles, no CDN. The bundle is
	// self-hosted; fonts use the system stack. Tightened vs the legacy panel.
	// base-uri 'self' (not 'none'): the SPA ships a static <base href> that the
	// server rewrites to the active WebBasePath — deep-link reloads depend on
	// it, and 'self' still blocks injected foreign-origin bases.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; script-src 'self'; style-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	if cache == "no-store" {
		w.Header().Set("Pragma", "no-cache")
	}
}
