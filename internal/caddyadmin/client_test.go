package caddyadmin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadConfigRejectsAdminStateWithDifferentDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			_, _ = w.Write([]byte(`{"apps":{"http":{}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	err := NewClient(srv.URL).LoadConfig([]byte(`{"apps":{}}`))
	if err == nil {
		t.Fatal("HTTP 200 was accepted without proving active Caddy config digest")
	}
	// The failure must be the digest mismatch, not a transport/decode error.
	if !strings.Contains(err.Error(), "caddy active config digest mismatch") {
		t.Fatalf("expected digest mismatch error, got %v", err)
	}
	if !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "observed") {
		t.Fatalf("digest mismatch error must name expected/observed digests, got %v", err)
	}
}

func TestLoadConfigPostsJSON(t *testing.T) {
	var received, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/load" {
				t.Errorf("path = %s", r.URL.Path)
			}
			contentType = r.Header.Get("Content-Type")
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			received = string(body)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.URL.Path != "/config/" {
				t.Errorf("verify path = %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(received))
		default:
			t.Errorf("method = %s", r.Method)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.LoadConfig([]byte(`{"apps":{}}`)); err != nil {
		t.Fatal(err)
	}
	// Caddy's /load endpoint requires application/json and the exact payload —
	// a non-empty body alone would green a mangled or re-encoded post (#885).
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if received != `{"apps":{}}` {
		t.Errorf("body = %q, want %q", received, `{"apps":{}}`)
	}
}
