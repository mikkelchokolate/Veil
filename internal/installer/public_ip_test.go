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
