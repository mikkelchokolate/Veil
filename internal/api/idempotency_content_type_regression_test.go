package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIdempotencyFingerprintIncludesNormalizedContentType(t *testing.T) {
	body := []byte(`{"confirm":true}`)
	jsonRequest := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	jsonRequest.Header.Set("Content-Type", "application/json")
	jsonCharset := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	jsonCharset.Header.Set("Content-Type", "application/json; charset=utf-8")
	plain := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	plain.Header.Set("Content-Type", "text/plain")
	missing := httptest.NewRequest(http.MethodPost, "/api/test", nil)

	jsonFP := idempotencyFingerprint(jsonRequest, body)
	charsetFP := idempotencyFingerprint(jsonCharset, body)
	plainFP := idempotencyFingerprint(plain, body)
	missingFP := idempotencyFingerprint(missing, body)

	if jsonFP != charsetFP {
		t.Fatalf("JSON media-type parameters should not change fingerprint: %s != %s", jsonFP, charsetFP)
	}
	if jsonFP == plainFP {
		t.Fatal("text/plain must not share the JSON fingerprint")
	}
	if jsonFP == missingFP {
		t.Fatal("missing Content-Type must not share the JSON fingerprint")
	}
}

func TestIdempotencyReplayDoesNotBypassIncompatibleContentType(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !isJSONMediaType(r.Header.Get("Content-Type")) {
			writeError(w, "Unsupported Media Type: Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"ok": true})
	}))
	issue := func(contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", strings.NewReader(`{"confirm":true}`))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Idempotency-Key", "create-client-ct")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	first := issue("application/json")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	replayJSON := issue("application/json; charset=utf-8")
	if replayJSON.Code != http.StatusCreated || replayJSON.Header().Get("Idempotency-Replayed") != "true" || calls.Load() != 1 {
		t.Fatalf("equivalent JSON replay status=%d replay=%q calls=%d body=%s", replayJSON.Code, replayJSON.Header().Get("Idempotency-Replayed"), calls.Load(), replayJSON.Body.String())
	}

	plain := issue("text/plain")
	if plain.Code != http.StatusConflict || plain.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatalf("incompatible Content-Type was replayed: status=%d replay=%q body=%s", plain.Code, plain.Header().Get("Idempotency-Replayed"), plain.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("incompatible Content-Type repeated mutation: calls=%d", calls.Load())
	}
}
