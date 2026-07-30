package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitForHealthyTimesOutWhenServerNeverResponds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := WaitForHealthy(server.URL, "", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForHealthyRetriesOnNon200(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := WaitForHealthy(server.URL, "", 3*time.Second)
	if err != nil {
		t.Fatalf("WaitForHealthy: %v", err)
	}
	if calls < 2 {
		t.Fatalf("server called %d times", calls)
	}
}

func TestWaitForHealthyHandlesRequestBuildError(t *testing.T) {
	err := WaitForHealthy("http://[::1%bad:80", "", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestWaitForHealthyReturnsWhenHTTPSucceedsWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Veil-Token") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := WaitForHealthy(server.URL, "", 3*time.Second); err != nil {
		t.Fatalf("WaitForHealthy: %v", err)
	}
}
