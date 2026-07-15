package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadOnlyPOSTAllowlistIncludesPanelPreviews(t *testing.T) {
	for _, path := range []string{
		"/api/apply/plan",
		"/api/client-links/qr",
		"/api/profiles/ru-recommended/preview",
		"/api/tools/dns-lookup",
		"/api/tools/ping",
		"/api/tools/speedtest",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if !isReadOnlyDiagnosticRequest(req) {
			t.Fatalf("%s should be available as a read-only POST", path)
		}
	}
}

func TestReadOnlyPOSTAllowlistExcludesMutations(t *testing.T) {
	for _, path := range []string{
		"/api/apply",
		"/api/version/update",
		"/api/inbounds",
		"/api/olcrtc/room",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if isReadOnlyDiagnosticRequest(req) {
			t.Fatalf("mutation %s must remain admin-only", path)
		}
	}

	wrongMethod := httptest.NewRequest(http.MethodGet, "/api/apply/plan", nil)
	if isReadOnlyDiagnosticRequest(wrongMethod) {
		t.Fatal("read-only POST allowlist must remain method-specific")
	}
}
