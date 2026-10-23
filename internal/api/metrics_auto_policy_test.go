package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// #1433: with --metrics-access auto on a loopback Panel the startup policy
// was resolved before any Panel user existed, so /metrics stayed public after
// first-run setup created one while every /api route required a session. The
// auto rule ("authenticate whenever auth is configured") must follow the live
// user set.
func TestMetricsAutoPolicyRequiresSessionOnceUsersExist(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	router, _ := newTestRouter(ServerInfo{
		Version:                        "test",
		StatePath:                      statePath,
		SetupAllowed:                   true,
		MetricsAuthWhenUsersConfigured: true,
	})

	if rec := getMetrics(router, nil); rec.Code != http.StatusOK {
		t.Fatalf("pre-setup /metrics = %d, want 200 on a never-configured loopback Panel: %s", rec.Code, rec.Body.String())
	}

	setup := postJSON(t, router, "/api/setup/complete",
		`{"username":"admin","password":"Sup3r-Secret-Passw0rd!","backupAcknowledged":true}`)
	if setup.Code != http.StatusOK && setup.Code != http.StatusCreated {
		t.Fatalf("setup complete = %d: %s", setup.Code, setup.Body.String())
	}

	if rec := getMetrics(router, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-setup anonymous /metrics = %d, want 401 once a Panel user exists: %s", rec.Code, rec.Body.String())
	}

	login := postJSON(t, router, "/api/auth/login",
		`{"username":"admin","password":"Sup3r-Secret-Passw0rd!"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	var session *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == "veil_session" {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("login did not issue a veil_session cookie")
	}
	if rec := getMetrics(router, session); rec.Code != http.StatusOK {
		t.Fatalf("post-setup session /metrics = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// An explicit --metrics-access public keeps loopback metrics anonymous even
// with users configured: only the auto policy follows the live user set.
func TestMetricsExplicitPublicStaysPublicWithUsers(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	router, _ := newTestRouter(ServerInfo{
		Version:      "test",
		StatePath:    statePath,
		SetupAllowed: true,
	})
	setup := postJSON(t, router, "/api/setup/complete",
		`{"username":"admin","password":"Sup3r-Secret-Passw0rd!","backupAcknowledged":true}`)
	if setup.Code != http.StatusOK && setup.Code != http.StatusCreated {
		t.Fatalf("setup complete = %d: %s", setup.Code, setup.Body.String())
	}
	if rec := getMetrics(router, nil); rec.Code != http.StatusOK {
		t.Fatalf("explicit public /metrics = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func getMetrics(router http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
