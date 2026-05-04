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
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer good.Close()

	ip, err := DetectPublicIP(context.Background(), good.Client(), []string{bad.URL, good.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
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
	// Endpoint returns documentation/test IP 203.0.113.10 — must be rejected.
	// Endpoint returns real public IP 93.184.216.34 — valid and should be returned.
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
	docTest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.10"))
	}))
	defer docTest.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34"))
	}))
	defer public.Close()

	ip, err := DetectPublicIP(context.Background(), public.Client(),
		[]string{private.URL, loopback.URL, linkLocal.URL, docTest.URL, public.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("expected 93.184.216.34, got %v", ip)
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

func TestDetectPublicIPRejectsDocumentationAddresses(t *testing.T) {
	// RFC 5737 TEST-NET-1, TEST-NET-2, TEST-NET-3 and RFC 2544 benchmark
	// addresses must all be rejected as non-public.
	docs := []string{
		"192.0.2.1",       // TEST-NET-1
		"198.51.100.42",   // TEST-NET-2
		"203.0.113.10",    // TEST-NET-3
		"198.18.0.1",      // RFC 2544 benchmark
	}
	for _, addr := range docs {
		addr := addr // capture loop variable
		doc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(addr))
		}))
		_, err := DetectPublicIP(context.Background(), doc.Client(), []string{doc.URL})
		doc.Close()
		if err == nil {
			t.Fatalf("expected error when endpoint returns documentation/reserved address %s", addr)
		}
	}
}

func TestDetectPublicIPNilContextDoesNotPanic(t *testing.T) {
	// When called with a nil context, DetectPublicIP should treat it as
	// context.Background() instead of panicking.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer server.Close()

	ip, err := DetectPublicIP(nil, server.Client(), []string{server.URL})
	if err != nil {
		t.Fatalf("unexpected error with nil context: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("unexpected IP with nil context: %v", ip)
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

func TestCidrInitDoesNotPanic(t *testing.T) {
	if cgnatCIDR == nil {
		t.Fatal("cgnatCIDR is nil after init")
	}
	if !cgnatCIDR.Contains(net.ParseIP("100.64.0.1")) {
		t.Fatal("expected 100.64.0.1 to be within CGNAT range")
	}
	if len(docCIDRs) != 4 {
		t.Fatalf("expected 4 doc CIDRs, got %d", len(docCIDRs))
	}
	for i, cidr := range docCIDRs {
		if cidr == nil {
			t.Fatalf("docCIDRs[%d] is nil after init", i)
		}
	}
	if !docCIDRs[0].Contains(net.ParseIP("192.0.2.1")) {
		t.Fatal("expected 192.0.2.1 to be within TEST-NET-1")
	}
}
