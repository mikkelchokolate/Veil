package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Issue #197: diagnostic tools must reject loopback, link-local (including
// cloud metadata), unspecified, and multicast targets — literal or resolved —
// before invoking ping or DNS lookups.
func TestPingRouteRejectsNonPublicLiteralTargets(t *testing.T) {
	old := pingRunner
	called := false
	pingRunner = func(host string, count int) PingResult {
		called = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })

	for _, target := range []string{
		"169.254.169.254",
		"fe80::1",
		"fe80::1%eth0",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"::",
		"224.0.0.1",
		"ff02::1",
		"::ffff:169.254.169.254",
	} {
		t.Run(target, func(t *testing.T) {
			called = false
			request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{"host": target, "count": 1})
			response := httptest.NewRecorder()
			DiagnosticToolRoutes{}.handlePing(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if called {
				t.Fatalf("ping runner invoked for rejected target %q", target)
			}
		})
	}
}

func TestPingRouteRejectsHostnameResolvingOnlyToForbiddenTargets(t *testing.T) {
	old := pingRunner
	pingCalled := false
	pingRunner = func(host string, count int) PingResult {
		pingCalled = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		if host == "metadata.google.internal" {
			return []net.IP{net.ParseIP("169.254.169.254")}, nil
		}
		return nil, &net.DNSError{IsNotFound: true}
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{"host": "metadata.google.internal", "count": 1})
	response := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handlePing(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if pingCalled {
		t.Fatal("ping runner invoked for metadata hostname")
	}
}

// A hostname resolving to a mix of allowed and forbidden addresses must be
// rejected: the OS may still pick the forbidden one when the tool runs
// (residual of #197).
func TestPingRouteRejectsHostnameResolvingToMixedPublicAndForbidden(t *testing.T) {
	old := pingRunner
	pingCalled := false
	pingRunner = func(host string, count int) PingResult {
		pingCalled = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		if host == "evil.example" {
			return []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("169.254.169.254")}, nil
		}
		return nil, &net.DNSError{IsNotFound: true}
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{"host": "evil.example", "count": 1})
	response := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handlePing(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if pingCalled {
		t.Fatal("ping runner invoked for mixed-resolution hostname")
	}
}

func TestDNSLookupRouteRejectsForbiddenTargets(t *testing.T) {
	old := dnsLookuper
	called := false
	dnsLookuper = func(host string) ([]string, string, error) {
		called = true
		return nil, "", nil
	}
	t.Cleanup(func() { dnsLookuper = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	for _, target := range []string{"169.254.169.254", "127.0.0.1", "::1", "metadata.google.internal"} {
		t.Run(target, func(t *testing.T) {
			called = false
			request := diagnosticJSONRequest(t, "/api/tools/dns-lookup", map[string]any{"hostname": target})
			response := httptest.NewRecorder()
			DiagnosticToolRoutes{}.handleDNSLookup(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if called {
				t.Fatalf("dns lookuper invoked for rejected target %q", target)
			}
		})
	}
}

func TestDiagnosticTargetsAllowPublicAndLANAddresses(t *testing.T) {
	old := pingRunner
	var got []string
	pingRunner = func(host string, count int) PingResult {
		got = append(got, host)
		return PingResult{Host: host}
	}
	t.Cleanup(func() { pingRunner = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("192.168.1.1")}, nil
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	for _, target := range []string{"8.8.8.8", "192.168.1.1", "10.20.30.40", "example.com"} {
		t.Run(target, func(t *testing.T) {
			request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{"host": target, "count": 1})
			response := httptest.NewRecorder()
			DiagnosticToolRoutes{}.handlePing(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if len(got) != 4 || strings.Join(got, ",") != "8.8.8.8,192.168.1.1,10.20.30.40,example.com" {
		t.Fatalf("ping targets=%v", got)
	}
}
