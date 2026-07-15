package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPingRouteNormalizesTargetAndDefaultsCount(t *testing.T) {
	old := pingRunner
	var gotHost string
	var gotCount int
	pingRunner = func(host string, count int) PingResult {
		gotHost = host
		gotCount = count
		return PingResult{Host: host, Transmitted: count}
	}
	t.Cleanup(func() { pingRunner = old })

	request := diagnosticJSONRequest(t, "/api/tools/ping", map[string]any{
		"host":  "  example.com  ",
		"count": 0,
	})
	response := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handlePing(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if gotHost != "example.com" || gotCount != 3 {
		t.Fatalf("pingRunner host=%q count=%d", gotHost, gotCount)
	}
}

func TestPingRouteRejectsOptionLikeTargetsAndInvalidCounts(t *testing.T) {
	old := pingRunner
	called := false
	pingRunner = func(host string, count int) PingResult {
		called = true
		return PingResult{}
	}
	t.Cleanup(func() { pingRunner = old })

	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{name: "option-like host", body: map[string]any{"host": "-f", "count": 1}, want: "must not begin"},
		{name: "whitespace in host", body: map[string]any{"host": "example .com", "count": 1}, want: "must not contain whitespace"},
		{name: "negative count", body: map[string]any{"host": "example.com", "count": -1}, want: "count must be 1-10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			request := diagnosticJSONRequest(t, "/api/tools/ping", tc.body)
			response := httptest.NewRecorder()
			DiagnosticToolRoutes{}.handlePing(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if called {
				t.Fatal("ping runner was called for an invalid request")
			}
		})
	}
}

func TestDNSLookupRouteNormalizesAndValidatesTarget(t *testing.T) {
	old := dnsLookuper
	var gotHost string
	dnsLookuper = func(host string) ([]string, string, error) {
		gotHost = host
		return []string{"203.0.113.1"}, "", nil
	}
	t.Cleanup(func() { dnsLookuper = old })

	request := diagnosticJSONRequest(t, "/api/tools/dns-lookup", map[string]any{"hostname": " example.com "})
	response := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handleDNSLookup(response, request)
	if response.Code != http.StatusOK || gotHost != "example.com" {
		t.Fatalf("status=%d host=%q body=%s", response.Code, gotHost, response.Body.String())
	}

	invalid := diagnosticJSONRequest(t, "/api/tools/dns-lookup", map[string]any{"hostname": "bad host"})
	invalidResponse := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handleDNSLookup(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func diagnosticJSONRequest(t *testing.T, path string, body any) *http.Request {
	t.Helper()
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(body); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &encoded)
	request.Header.Set("Content-Type", "application/json")
	return request
}
