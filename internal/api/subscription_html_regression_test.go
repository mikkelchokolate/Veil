package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWantsHTMLRequiresDocumentNavigation(t *testing.T) {
	htmlAccept := httptest.NewRequest(http.MethodGet, "/s/token", nil)
	htmlAccept.Header.Set("Accept", "text/html")
	if wantsHTML(htmlAccept) {
		t.Fatal("Accept: text/html without Sec-Fetch-Dest=document must stay on the machine feed")
	}

	importer := httptest.NewRequest(http.MethodGet, "/s/token", nil)
	importer.Header.Set("Accept", "text/html,*/*")
	if wantsHTML(importer) {
		t.Fatal("importer Accept: text/html,*/* must stay on the machine feed")
	}

	raw := httptest.NewRequest(http.MethodGet, "/s/token?format=raw", nil)
	raw.Header.Set("Accept", "text/html")
	raw.Header.Set("Sec-Fetch-Dest", "document")
	if wantsHTML(raw) {
		t.Fatal("explicit format must not take the HTML landing")
	}

	browser := httptest.NewRequest(http.MethodGet, "/s/token", nil)
	browser.Header.Set("Accept", "text/html")
	browser.Header.Set("Sec-Fetch-Dest", "document")
	if !wantsHTML(browser) {
		t.Fatal("browser document navigation must keep the HTML landing")
	}
}

func TestPublicSubscriptionHTMLLandingIsInline(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Dest", "document")
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

func TestPublicSubscriptionHTMLAcceptWithoutDocumentDestIsMachineFeed(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	plaintext, _ := seedClientWithToken(t, router)

	for _, accept := range []string{"text/html", "text/html,*/*"} {
		req := httptest.NewRequest(http.MethodGet, "/s/"+plaintext, nil)
		req.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("accept %q: %d %s", accept, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
			t.Fatalf("accept %q content-type = %q, want text/plain", accept, ct)
		}
		if strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
			t.Fatalf("accept %q served HTML landing to a non-document client: %q", accept, w.Body.String())
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Fatalf("accept %q content-disposition = %q", accept, cd)
		}
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
