package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// diagnosticPOSTPaths is the set of POST endpoints that legitimately serve
// viewers: they compute a result without mutating state. Anything else that
// looks "read-only-ish" (notably /api/client-links/qr, which renders
// subscription secrets) must keep its admin capability.
var diagnosticPOSTPaths = []string{
	"/api/apply/plan",
	"/api/profiles/ru-recommended/preview",
	"/api/tools/dns-lookup",
	"/api/tools/ping",
	"/api/tools/speedtest",
}

// TestDiagnosticPOSTsUseViewerPolicy checks the live authorization table —
// the dead isReadOnlyDiagnosticRequest helper it replaced never ran in
// production, so the policy itself is what must be locked (issue #880).
func TestDiagnosticPOSTsUseViewerPolicy(t *testing.T) {
	for _, path := range diagnosticPOSTPaths {
		capability, known := capabilityForEndpoint(http.MethodPost, path)
		if !known {
			t.Errorf("POST %s has no endpoint policy", path)
			continue
		}
		if capability != capabilityViewer {
			t.Errorf("POST %s capability=%s, want viewer diagnostic", path, capability)
		}
	}
}

// TestClientLinkQRIsAdminSecretNotDiagnostic is the authorization regression:
// the QR endpoint renders the raw subscription URI, so it is admin-secret
// material — it must never be classified as a viewer read-only diagnostic.
func TestClientLinkQRIsAdminSecretNotDiagnostic(t *testing.T) {
	capability, known := capabilityForEndpoint(http.MethodPost, "/api/client-links/qr")
	if !known {
		t.Fatal("POST /api/client-links/qr has no endpoint policy")
	}
	if capability != capabilityAdminSecret {
		t.Fatalf("POST /api/client-links/qr capability=%s, want admin-secret", capability)
	}

	state := &managementState{
		users:    []User{{Username: "viewer", Role: "viewer"}},
		sessions: mustNewSessionRegistry(""),
	}
	viewer := mustCreateSession(t, state.sessions, "viewer", "viewer")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             "admin-token",
		AllowDevAnonymous: false,
		AllowSetup:        true,
	}, next)

	// Anonymous callers are rejected before the capability check.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/client-links/qr", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d want=401 body=%s", rec.Code, rec.Body.String())
	}

	// A viewer session with a valid CSRF token still lacks the capability.
	req := httptest.NewRequest(http.MethodPost, "/api/client-links/qr", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
	req.Header.Set("X-CSRF-Token", viewer.CSRFToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d want=403 body=%s", rec.Code, rec.Body.String())
	}

	// The admin token reaches the handler.
	req = httptest.NewRequest(http.MethodPost, "/api/client-links/qr", nil)
	req.Header.Set("X-Veil-Token", "admin-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin status=%d want=204 body=%s", rec.Code, rec.Body.String())
	}
}

// TestDiagnosticPOSTsRequireCSRFThroughMiddleware proves the viewer-diagnostic
// POSTs still enforce CSRF for cookie sessions: read-only semantics do not
// waive the mutation guard.
func TestDiagnosticPOSTsRequireCSRFThroughMiddleware(t *testing.T) {
	state := &managementState{
		users:    []User{{Username: "viewer", Role: "viewer"}},
		sessions: mustNewSessionRegistry(""),
	}
	viewer := mustCreateSession(t, state.sessions, "viewer", "viewer")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             "admin-token",
		AllowDevAnonymous: false,
		AllowSetup:        true,
	}, next)

	for _, path := range diagnosticPOSTPaths {
		// No CSRF header: rejected before reaching the handler.
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s without CSRF: status=%d want=403", path, rec.Code)
		}

		// Valid CSRF: passes authz and reaches the handler.
		req = httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
		req.Header.Set("X-CSRF-Token", viewer.CSRFToken)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("POST %s with CSRF: status=%d want=204 body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestMutationsStayAdminThroughMiddleware proves actual state mutations do not
// slip into the viewer-diagnostic bucket.
func TestMutationsStayAdminThroughMiddleware(t *testing.T) {
	for _, path := range []string{
		"/api/apply",
		"/api/version/update",
		"/api/inbounds",
		"/api/protocols/olcrtc/room",
	} {
		capability, known := capabilityForEndpoint(http.MethodPost, path)
		if !known {
			t.Errorf("POST %s has no endpoint policy", path)
			continue
		}
		if capabilityAllowsRole(capability, "viewer") {
			t.Errorf("POST %s capability=%s must not allow viewer role", path, capability)
		}
	}
}
