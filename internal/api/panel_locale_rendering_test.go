package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestAuthenticatedUserLocaleWinsWhenRenderingPanel(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		users: []model.User{{
			Username: "alice",
			Role:     "viewer",
			Locale:   "ru",
		}},
	}
	routes := PanelRoutes{State: state}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req.AddCookie(&http.Cookie{Name: "veil_locale", Value: "en"})
	rec := httptest.NewRecorder()

	routes.handlePanel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`<html lang="ru">`, `window.veilLocale = "ru"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("Panel HTML missing %q", want)
		}
	}
}
