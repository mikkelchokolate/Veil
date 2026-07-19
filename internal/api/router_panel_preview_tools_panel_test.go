package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The React SPA is the primary panel UI (B11). The shell at "/" is the SPA
// index: self-hosted assets, no-store, tightened CSP (no unsafe-inline, no
// CDN). The legacy server-rendered panel was superseded.
func TestRouterServesPanelShell(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			r, _ := NewRouter(ServerInfo{Version: "test"})
			req := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Fatalf("unexpected content-type: %q", ct)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("expected no-store cache-control for panel shell, got %q", cc)
			}
			if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
				t.Fatalf("expected nosniff for panel shell, got %q", nosniff)
			}
			csp := w.Header().Get("Content-Security-Policy")
			if strings.Contains(csp, "unsafe-inline") {
				t.Fatalf("SPA CSP must not allow unsafe-inline: %q", csp)
			}
			if !strings.Contains(csp, "script-src 'self'") {
				t.Fatalf("SPA CSP must be self-hosted scripts: %q", csp)
			}
			if referrer := w.Header().Get("Referrer-Policy"); referrer != "no-referrer" {
				t.Fatalf("unexpected panel referrer-policy: %q", referrer)
			}
			if xfo := w.Header().Get("X-Frame-Options"); xfo != "DENY" {
				t.Fatalf("unexpected panel x-frame-options: %q", xfo)
			}
			if method == http.MethodGet {
				body := w.Body.String()
				if !strings.Contains(body, `<base href="/"`) {
					t.Fatalf("SPA shell must carry a <base href>: %s", body)
				}
				if !strings.Contains(body, `id="root"`) {
					t.Fatalf("SPA shell must mount the React root: %s", body)
				}
			}
		})
	}
}

// SPA client-side routes render the same shell (history-API fallback).
func TestRouterServesPanelShellForClientRoutes(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	for _, p := range []string{"/clients", "/clients/abc", "/inbounds", "/apply", "/traffic"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("client route %q: expected 200, got %d", p, w.Code)
		}
		if !strings.Contains(w.Body.String(), `id="root"`) {
			t.Fatalf("client route %q must render SPA shell", p)
		}
	}
}
