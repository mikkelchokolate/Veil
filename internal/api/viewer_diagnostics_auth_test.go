package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestViewerMayRunReadOnlyPostsButNotMutations(t *testing.T) {
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
		"/api/backups/veil-2026-07-10.tar.gz.age/verify",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("viewer read-only POST %s expected 200, got %d", path, w.Code)
			}
		})
	}

	for _, path := range []string{
		"/api/inbounds",
		"/api/backups",
		"/api/backups/prune",
		"/api/backups/archive/restore",
		"/api/backups/nested/archive/verify",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("viewer mutation %s expected 403, got %d", path, w.Code)
		}
	}
}

func TestViewerReadOnlyPostStillRequiresCSRF(t *testing.T) {
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
		t.Fatalf("viewer read-only POST without CSRF expected 403, got %d", w.Code)
	}
}
