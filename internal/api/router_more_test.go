package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthClassifiesPathsAfterWebBasePathStrip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/s/public-token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	state := &managementState{}
	authenticated := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             "admin-token",
		AllowDevAnonymous: false,
	}, mux)
	handler := stripBasePathMiddleware("/secret/", authenticated)

	anonymous := httptest.NewRequest(http.MethodGet, "/secret/api/settings", nil)
	anonymousRec := httptest.NewRecorder()
	handler.ServeHTTP(anonymousRec, anonymous)
	if anonymousRec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous prefixed API status=%d want=401 body=%s", anonymousRec.Code, anonymousRec.Body.String())
	}

	authed := httptest.NewRequest(http.MethodGet, "/secret/api/settings", nil)
	authed.Header.Set("X-Veil-Token", "admin-token")
	authedRec := httptest.NewRecorder()
	handler.ServeHTTP(authedRec, authed)
	if authedRec.Code != http.StatusNoContent {
		t.Fatalf("token prefixed API status=%d want=204 body=%s", authedRec.Code, authedRec.Body.String())
	}

	unprefixed := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	unprefixedRec := httptest.NewRecorder()
	handler.ServeHTTP(unprefixedRec, unprefixed)
	if unprefixedRec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed API under secret mount status=%d want=404 body=%s", unprefixedRec.Code, unprefixedRec.Body.String())
	}

	subscription := httptest.NewRequest(http.MethodGet, "/s/public-token", nil)
	subscriptionRec := httptest.NewRecorder()
	handler.ServeHTTP(subscriptionRec, subscription)
	if subscriptionRec.Code != http.StatusNoContent {
		t.Fatalf("host-root subscription status=%d want=204 body=%s", subscriptionRec.Code, subscriptionRec.Body.String())
	}
}

func TestStripBasePathMiddleware(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"path": r.URL.Path})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"path": r.URL.Path})
	})

	handler := stripBasePathMiddleware("/secret/", mux)

	cases := []struct {
		path       string
		wantStatus int
		wantPath   string
	}{
		{"/secret/api/version", http.StatusOK, "/api/version"},
		{"/secret", http.StatusOK, "/"},
		{"/secret/", http.StatusOK, "/"},
		{"/other/api/version", http.StatusNotFound, ""},
		{"/secretary/api/version", http.StatusNotFound, ""},
		{"/secret-other", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
			}
			if tc.wantPath != "" && !strings.Contains(rec.Body.String(), tc.wantPath) {
				t.Fatalf("%s body=%s", tc.path, rec.Body.String())
			}
		})
	}
}

// Regression for #662: the /s/{token} bypass must match only the exact
// single-segment public-subscription shape. Anything deeper under /s/ still
// goes through the mount check, so an /s/-nested mount cannot blackhole
// panel API paths and a multi-segment /s/ path under a normal mount is a
// plain 404 instead of bypassing the strip.
func TestStripBasePathSubscriptionBypassShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"path": r.URL.Path})
	})
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"path": r.URL.Path})
	})

	t.Run("public feed bypasses a normal mount", func(t *testing.T) {
		handler := stripBasePathMiddleware("/secret/", mux)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/s/public-token", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/s/public-token") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("multi-segment /s path does not bypass", func(t *testing.T) {
		handler := stripBasePathMiddleware("/secret/", mux)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/s/token/extra", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=404 body=%s", rec.Code, rec.Body.String())
		}
	})

	// A mount under /s/ is rejected by webbasepath validation, but the
	// middleware must still behave sanely if such a mount is ever
	// constructed directly: nested panel paths strip, and only the bare
	// /s/{token} shape reaches the subscription route.
	t.Run("nested mount under /s still strips api paths", func(t *testing.T) {
		handler := stripBasePathMiddleware("/s/panel/", mux)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/s/panel/api/version", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/api/version") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestDecodeJSONRequestRejectsNonJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	var v struct{}
	if decodeJSONRequest(rec, req, &v) {
		t.Fatal("expected decode to fail for non-JSON content type")
	}
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestDecodeJSONRequestRejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"unknown":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	var v struct{}
	if decodeJSONRequest(rec, req, &v) {
		t.Fatal("expected decode to fail for unknown field")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestDecodeJSONRequestRejectsMultipleValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{}{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	var v struct{}
	if decodeJSONRequest(rec, req, &v) {
		t.Fatal("expected decode to fail for multiple JSON values")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecodeJSONRequestRejectsOversizedBody(t *testing.T) {
	body := []byte(fmt.Sprintf(`{"a":"%s"}`, strings.Repeat("x", int(maxJSONBodyBytes)+1)))
	req := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	var v struct{}
	if decodeJSONRequest(rec, req, &v) {
		t.Fatal("expected decode to fail for oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", rec.Code)
	}
}
