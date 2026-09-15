package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitForHealthyWithoutSchemeTriesHTTPFallbackOnLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := WaitForHealthy(server.Listener.Addr().String(), "", time.Second); err != nil {
		t.Fatalf("WaitForHealthy should fall back to HTTP on loopback listen without scheme: %v", err)
	}
}

func TestWaitForHealthySendsAuthToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Veil-Token") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := WaitForHealthy(server.URL, "secret", time.Second); err != nil {
		t.Fatalf("WaitForHealthy should send token: %v", err)
	}
}

func TestWaitForHealthyAtUsesWebBasePath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path != "/secret-panel/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := WaitForHealthyAt(server.URL, "", "/secret-panel/", time.Second); err != nil {
		t.Fatalf("WaitForHealthyAt prefixed healthz: %v (path=%q)", err, gotPath)
	}
	if gotPath != "/secret-panel/healthz" {
		t.Fatalf("path = %q, want /secret-panel/healthz", gotPath)
	}
}
