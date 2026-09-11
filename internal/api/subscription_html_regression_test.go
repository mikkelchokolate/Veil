package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicSubscriptionHTMLLandingIsInline(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("html: %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); strings.Contains(strings.ToLower(cd), "attachment") {
		t.Fatalf("html landing inherited attachment disposition: %q", cd)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q", cc)
	}
	if !strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Fatalf("expected html body, got %q", w.Body.String())
	}
}

func TestPublicSubscriptionRawKeepsAttachmentDisposition(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=raw", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("raw: %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("raw content-disposition = %q", cd)
	}
}

func TestPublicSubscriptionBase64KeepsAttachmentDisposition(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext+"?format=base64", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("base64: %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("base64 content-disposition = %q", cd)
	}
}
