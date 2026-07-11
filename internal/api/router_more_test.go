package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
