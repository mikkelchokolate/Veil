package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitForHealthyWithoutSchemeTriesGeneratedPanelTLSOnLoopback(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := WaitForHealthy(server.Listener.Addr().String(), "", time.Second); err != nil {
		t.Fatalf("WaitForHealthy should try generated Panel TLS on loopback listen without scheme: %v", err)
	}
}

func TestWaitForHealthySupportsGeneratedPanelTLSOnLoopback(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := WaitForHealthy(server.URL, "", time.Second); err != nil {
		t.Fatalf("WaitForHealthy should trust generated Panel TLS on loopback: %v", err)
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
