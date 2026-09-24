package caddyadmin

import (
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
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/load" {
				t.Errorf("path = %s", r.URL.Path)
			}
			buf := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buf)
			received = string(buf)
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
	if received == "" {
		t.Error("server received empty body")
	}
}
