package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestViewerMayRunReadOnlyDiagnosticsButNotMutations(t *testing.T) {
	state := &managementState{users: []User{{Username: "viewer", Role: "viewer"}}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(state, "", next)
	session := globalSessions.NewSession("viewer", "viewer")
	defer globalSessions.Delete(session.Token)

	for _, path := range []string{
		"/api/tools/dns-lookup",
		"/api/tools/ping",
		"/api/tools/speedtest",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("viewer diagnostic POST %s expected 200, got %d", path, w.Code)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation expected 403, got %d", w.Code)
	}
}

func TestViewerDiagnosticPostStillRequiresCSRF(t *testing.T) {
	state := &managementState{users: []User{{Username: "viewer", Role: "viewer"}}}
	handler := authMiddleware(state, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	session := globalSessions.NewSession("viewer", "viewer")
	defer globalSessions.Delete(session.Token)

	req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer diagnostic without CSRF expected 403, got %d", w.Code)
	}
}
