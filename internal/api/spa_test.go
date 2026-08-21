package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSPAServesIndexWithBasePath verifies the SPA shell is served at "/" with
// the <base href> rewritten to the panel WebBasePath (B3/B11).
func TestSPAServesIndexWithBasePath(t *testing.T) {
	h, err := newSPAHandler("/secretpath/")
	if err != nil {
		t.Skipf("SPA bundle not embedded (run web build): %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.serveIndex(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<base href="/secretpath/" />`) {
		t.Fatalf("base href not rewritten to WebBasePath; got: %.200s", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("index must be no-store, got %q", cc)
	}
}

func TestSPAIndexGzipWhenRequested(t *testing.T) {
	h, err := newSPAHandler("/")
	if err != nil {
		t.Skipf("SPA bundle not embedded (run web build): %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.serveIndex(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
	body := rec.Body.Bytes()
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		t.Fatalf("response is not gzip: %x", body[:min(8, len(body))])
	}
}

func TestSPAIndexUncompressedWithoutAcceptGzip(t *testing.T) {
	h, err := newSPAHandler("/")
	if err != nil {
		t.Skipf("SPA bundle not embedded (run web build): %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.serveIndex(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("did not request gzip, got encoding %q", rec.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(rec.Body.String(), "<title>Veil</title>") {
		t.Fatal("uncompressed index missing title")
	}
	if !strings.Contains(rec.Body.String(), `name="description"`) {
		t.Fatal("index missing meta description")
	}
}

// TestSPAClientRouteFallback serves the shell for client-side routes.
func TestSPAClientRouteFallback(t *testing.T) {
	h, err := newSPAHandler("/")
	if err != nil {
		t.Skipf("SPA bundle not embedded: %v", err)
	}
	if !h.matches("/clients") {
		t.Fatal("client route /clients should match SPA")
	}
	for _, p := range []string{"/api/v1/clients", "/s/token", "/assets/app.js", "/metrics"} {
		if h.matches(p) {
			t.Fatalf("path %q must NOT match SPA shell", p)
		}
	}
}
