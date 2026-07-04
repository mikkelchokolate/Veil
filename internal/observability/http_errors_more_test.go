package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMethodNotAllowedSetsAllowAndReturns405(t *testing.T) {
	rec := httptest.NewRecorder()
	methodNotAllowed(rec, http.MethodGet, http.MethodPost)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	allow := rec.Header().Get("Allow")
	if allow != "GET, POST" {
		t.Fatalf("Allow header = %q, want %q", allow, "GET, POST")
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "method not allowed" {
		t.Fatalf("body = %q, want %q", body, "method not allowed")
	}
}
