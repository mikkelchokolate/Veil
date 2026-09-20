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

// #374: a hostname whose resolution fails must be rejected — allowing it
// through would let the tool re-resolve (possibly to a forbidden target)
// after the scope check.
func TestPingRouteFailsClosedOnLookupError(t *testing.T) {
	old := pingRunner
	pingCalled := false
	pingRunner = func(host string, count int) PingResult {
		pingCalled = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return nil, &net.DNSError{IsNotFound: true}
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{"host": "gone.example", "count": 1})
	response := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handlePing(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if pingCalled {
		t.Fatal("ping runner invoked despite lookup failure")
	}
}

// #357: a hostname resolving to ANY forbidden address must be rejected — the
// OS can pick the forbidden entry at dial time even when another record is
// allowed.
func TestPingRouteRejectsHostnameWithMixedResolution(t *testing.T) {
	old := pingRunner
	pingCalled := false
	pingRunner = func(host string, count int) PingResult {
		pingCalled = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("169.254.169.254")}, nil
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	for _, route := range []struct {
		path  string
		key   string
		apply func(http.ResponseWriter, *http.Request)
	}{
		{"/api/tools/ping", "host", DiagnosticToolRoutes{}.handlePing},
		{"/api/tools/dns-lookup", "hostname", DiagnosticToolRoutes{}.handleDNSLookup},
	} {
		request := diagnosticJSONRequest(t, route.path, map[string]any{route.key: "mixed.example", "count": 1})
		response := httptest.NewRecorder()
		route.apply(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", route.path, response.Code, response.Body.String())
		}
	}
	if pingCalled {
		t.Fatal("ping runner invoked for mixed-resolution hostname")
	}
}

// #574: inet_aton shorthand is not a canonical literal but IS treated as an
// address by ping — canonicalize before the scope check so short/decimal/hex
// forms cannot smuggle loopback or link-local destinations past it.
func TestPingRouteRejectsIPv4ShorthandForForbiddenTargets(t *testing.T) {
	old := pingRunner
	pingCalled := false
	pingRunner = func(host string, count int) PingResult {
		pingCalled = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })
	oldLookup := diagnosticTargetLookup
	diagnosticTargetLookup = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}
	t.Cleanup(func() { diagnosticTargetLookup = oldLookup })

	for _, target := range []string{
		"127.1",      // 127.0.0.1
		"2130706433", // 127.0.0.1 decimal
		"0x7f000001", // 127.0.0.1 hex
		"0177.0.0.1", // 127.0.0.1 octal
		"169.254.169.254",
		"0xa9fea9fe",      // 169.254.169.254 hex
		"2852039166",      // 169.254.169.254 decimal
		"0",               // 0.0.0.0
		"0300.0250.01.01", // 192.168.1.1 in octal — public-policy LAN addr, NOT rejected
	} {
		pingCalled = false
		request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{"host": target, "count": 1})
		response := httptest.NewRecorder()
		DiagnosticToolRoutes{}.handlePing(response, request)
		if target == "0300.0250.01.01" {
			// Octal shorthand for a LAN address stays allowed and is pinned to
			// its canonical literal.
			if response.Code != http.StatusOK {
				t.Fatalf("%s status=%d body=%s, want 200", target, response.Code, response.Body.String())
			}
			continue
		}
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s, want 400", target, response.Code, response.Body.String())
		}
		if pingCalled {
			t.Fatalf("ping runner invoked for shorthand target %q", target)
		}
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
	// Hostnames are pinned to the scope-approved literal: ping must receive the
	// first resolved address, never the rebindable hostname (#575).
	if len(got) != 4 || strings.Join(got, ",") != "8.8.8.8,192.168.1.1,10.20.30.40,203.0.113.10" {
		t.Fatalf("ping targets=%v", got)
	}
}
