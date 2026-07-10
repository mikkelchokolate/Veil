package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadOnlyPOSTAllowlistIncludesApplyPlanOnly(t *testing.T) {
	allowed := httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil)
	if !isReadOnlyDiagnosticRequest(allowed) {
		t.Fatal("apply plan should be available as a read-only POST")
	}

	mutation := httptest.NewRequest(http.MethodPost, "/api/apply", nil)
	if isReadOnlyDiagnosticRequest(mutation) {
		t.Fatal("real apply operation must remain admin-only")
	}

	wrongMethod := httptest.NewRequest(http.MethodGet, "/api/apply/plan", nil)
	if isReadOnlyDiagnosticRequest(wrongMethod) {
		t.Fatal("read-only POST allowlist must remain method-specific")
	}
}
