package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEffectiveAuthStatusPrefersValidStaticTokenOverViewerCookie(t *testing.T) {
	registry := mustNewSessionRegistry("")
	viewer, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatalf("Create viewer session: %v", err)
	}
	state := &managementState{authToken: "static-secret", sessions: registry}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.Header.Set("X-Veil-Token", "static-secret")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
	rec := httptest.NewRecorder()

	state.handleEffectiveAuthStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"authenticated":true`, `"username":"api-token"`, `"role":"admin"`, `"authMethod":"static-token"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("body missing %q: %s", want, rec.Body.String())
		}
	}
}

func TestEffectiveAuthStatusFallsBackToCookieForInvalidStaticToken(t *testing.T) {
	registry := mustNewSessionRegistry("")
	viewer, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatalf("Create viewer session: %v", err)
	}
	state := &managementState{authToken: "static-secret", sessions: registry}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.Header.Set("X-Veil-Token", "wrong-token")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
	rec := httptest.NewRecorder()

	state.handleEffectiveAuthStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"authenticated":true`, `"username":"viewer"`, `"role":"viewer"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("body missing %q: %s", want, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), `"authMethod":"static-token"`) {
		t.Fatalf("invalid static token must not receive token auth status: %s", rec.Body.String())
	}
}

func TestEffectiveAuthStatusRejectsNonGetMethods(t *testing.T) {
	state := &managementState{authToken: "static-secret", sessions: mustNewSessionRegistry("")}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	state.handleEffectiveAuthStatus(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
