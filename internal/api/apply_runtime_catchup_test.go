package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestAuthLocaleDoesNotLeaveApplyPending(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	settings := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`,
	))
	settings.Header.Set("Content-Type", "application/json")
	settingsRec := httptest.NewRecorder()
	router.ServeHTTP(settingsRec, settings)
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("seed settings: %d %s", settingsRec.Code, settingsRec.Body.String())
	}
	desiredBefore, appliedBefore := applyState(t, router)
	if desiredBefore != appliedBefore {
		t.Fatalf("baseline not synced: desired=%d applied=%d", desiredBefore, appliedBefore)
	}

	state.mu.Lock()
	state.users = []model.User{{
		Username:     "alice",
		PasswordHash: "hash",
		Role:         "admin",
		Locale:       "en",
	}}
	state.mu.Unlock()

	session, err := state.sessions.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/locale", strings.NewReader(`{"locale":"ru"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, "alice"))
	rec := httptest.NewRecorder()
	state.handleAuthLocale(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("locale: %d %s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	rev, err := state.applyRevisions.Get()
	view := state.applyStateViewLocked()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if rev.Desired <= desiredBefore {
		t.Fatalf("locale should pin a new desired revision: before=%d after=%d", desiredBefore, rev.Desired)
	}
	if rev.Desired != rev.Applied {
		t.Fatalf("locale left apply pending: desired=%d applied=%d", rev.Desired, rev.Applied)
	}
	if view.State != apply.StateSynced {
		t.Fatalf("apply state after locale = %q, want synced", view.State)
	}
	if state.users[0].Locale != "ru" {
		t.Fatalf("locale not persisted: %+v", state.users[0])
	}
}

func TestInboundMutationStillPendingWhenAutoApplyDisabled(t *testing.T) {
	router, _ := newApplyTrackedRouterWithState(t)

	settings := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}`,
	))
	settings.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), settings)
	desiredBefore, appliedBefore := applyState(t, router)
	if desiredBefore != appliedBefore {
		t.Fatalf("baseline not synced: desired=%d applied=%d", desiredBefore, appliedBefore)
	}
	autoApplyAfterMutation = false

	body := strings.NewReader(`{"name":"hy2-pending","protocol":"hysteria2","transport":"udp","port":9443,"enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", rec.Code, rec.Body.String())
	}
	desired, applied := applyState(t, router)
	if desired == applied {
		t.Fatalf("runtime mutation incorrectly caught up without apply: desired=%d applied=%d", desired, applied)
	}
}
