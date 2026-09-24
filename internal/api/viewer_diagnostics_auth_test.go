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
	session := mustCreateSession(t, globalSessions, "viewer", "viewer")
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

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/inbounds"},
		{http.MethodPost, "/api/backups"},
		{http.MethodPost, "/api/backups/archive/verify"},
		// Users mutations are admin-only (#838): a viewer with valid CSRF must
		// still be denied on create/update/delete.
		{http.MethodPost, "/api/users"},
		{http.MethodPut, "/api/users/admin"},
		{http.MethodDelete, "/api/users/admin"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("viewer mutation %s %s expected 403, got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestViewerDiagnosticPostStillRequiresCSRF(t *testing.T) {
	state := &managementState{users: []User{{Username: "viewer", Role: "viewer"}}}
	handler := authMiddleware(state, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	session := mustCreateSession(t, globalSessions, "viewer", "viewer")
	defer globalSessions.Delete(session.Token)

	req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer diagnostic without CSRF expected 403, got %d", w.Code)
	}
}
