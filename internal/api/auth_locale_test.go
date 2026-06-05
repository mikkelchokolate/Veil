package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestAuthLocaleUpdatesCurrentViewerPreference(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		statePath: filepath.Join(t.TempDir(), "state.json"),
		sessions:  registry,
		users: []model.User{{
			Username:     "viewer",
			PasswordHash: "hash",
			Role:         "viewer",
			Locale:       "en",
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/locale", strings.NewReader(`{"locale":"ru"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, "viewer"))
	rec := httptest.NewRecorder()

	state.handleAuthLocale(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].Locale != "ru" {
		t.Fatalf("user=%+v", state.users[0])
	}
	var response struct {
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Locale != "ru" {
		t.Fatalf("response=%+v", response)
	}
	foundCookie := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "veil_locale" && cookie.Value == "ru" {
			foundCookie = true
		}
	}
	if !foundCookie {
		t.Fatal("locale cookie was not set")
	}
}

func TestAuthLocaleRejectsStaticTokenWithoutUserSession(t *testing.T) {
	state := &managementState{users: []model.User{{
		Username: "admin",
		Role:     "admin",
		Locale:   "en",
	}}}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/locale", strings.NewReader(`{"locale":"ru"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, "api-token"))
	rec := httptest.NewRecorder()

	state.handleAuthLocale(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if state.users[0].Locale != "en" {
		t.Fatalf("user locale changed: %+v", state.users[0])
	}
}
