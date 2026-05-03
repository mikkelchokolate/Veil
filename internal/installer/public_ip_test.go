package installer

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDetectPublicIPReturnsFirstValidEndpoint(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not an ip"))
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.10\n"))
	}))
	defer good.Close()

	ip, err := DetectPublicIP(context.Background(), good.Client(), []string{bad.URL, good.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("203.0.113.10")) {
		t.Fatalf("unexpected IP: %v", ip)
	}
}

func TestDetectPublicIPFailsWhenNoEndpointReturnsValidIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not an ip"))
	}))
	defer server.Close()

	if _, err := DetectPublicIP(context.Background(), server.Client(), []string{server.URL}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestDetectPublicIPRejectsNonPublicAddresses(t *testing.T) {
	// Endpoint returns private IP 10.0.0.1 — must be rejected.
	// Endpoint returns loopback 127.0.0.1 — must be rejected.
	// Endpoint returns public 203.0.113.10 — valid and should be returned.
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("10.0.0.1"))
	}))
	defer private.Close()
	loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("127.0.0.1"))
	}))
	defer loopback.Close()
	linkLocal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("169.254.1.1"))
	}))
	defer linkLocal.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.10"))
	}))
	defer public.Close()

	ip, err := DetectPublicIP(context.Background(), public.Client(),
		[]string{private.URL, loopback.URL, linkLocal.URL, public.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("203.0.113.10")) {
		t.Fatalf("expected 203.0.113.10, got %v", ip)
	}
}

func TestDetectPublicIPFailsWhenAllEndpointsReturnNonPublic(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("10.0.0.1"))
	}))
	defer private.Close()

	_, err := DetectPublicIP(context.Background(), private.Client(), []string{private.URL})
	if err == nil {
		t.Fatalf("expected error when all endpoints return non-public IPs")
	}
}

func TestDetectPublicIPRejectsCGNATAddresses(t *testing.T) {
	cgnat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("100.64.0.1"))
	}))
	defer cgnat.Close()

	_, err := DetectPublicIP(context.Background(), cgnat.Client(), []string{cgnat.URL})
	if err == nil {
		t.Fatalf("expected error when endpoint returns CGNAT address 100.64.0.1")
	}
}

func TestDefaultPublicIPEndpointsAreHTTPS(t *testing.T) {
	endpoints := DefaultPublicIPEndpoints()
	if len(endpoints) == 0 {
		t.Fatalf("expected endpoints")
	}
	for _, endpoint := range endpoints {
		if len(endpoint) < len("https://") || endpoint[:len("https://")] != "https://" {
			t.Fatalf("endpoint must use https: %s", endpoint)
		}
	}
}
