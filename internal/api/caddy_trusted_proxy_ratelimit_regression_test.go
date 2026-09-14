package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newCaddyPanelRouter(t *testing.T) (http.Handler, *managementState) {
	t.Helper()
	router, reloader := newTestRouter(ServerInfo{
		Version:     "test",
		Mode:        "server",
		ApplyRoot:   t.TempDir(),
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:2096",
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	return router, state
}

func caddyLoginRequest(username, forwardedFor string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(
		`{"username":"`+username+`","password":"wrong-password"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", forwardedFor)
	return req
}

func TestCaddyPanelLoginRateLimitUsesForwardedClient(t *testing.T) {
	router, _ := newCaddyPanelRouter(t)

	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, caddyLoginRequest(fmt.Sprintf("operator-%d", i), fmt.Sprintf("203.0.113.%d", i+1)))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("distinct forwarded client %d shared the Caddy loopback bucket: %s", i, rec.Body.String())
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("distinct forwarded client %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	var last *httptest.ResponseRecorder
	for i := 0; i < 4; i++ {
		last = httptest.NewRecorder()
		router.ServeHTTP(last, caddyLoginRequest(fmt.Sprintf("same-client-%d", i), "198.51.100.20"))
		if i < 3 && last.Code == http.StatusTooManyRequests {
			t.Fatalf("same forwarded client request %d was limited before burst: %s", i, last.Body.String())
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("fourth login from the same forwarded client status=%d body=%s", last.Code, last.Body.String())
	}
}

func TestCaddyPanelSubscriptionAndAuditUseForwardedClient(t *testing.T) {
	router, state := newCaddyPanelRouter(t)
	now := time.Now()
	for i := 0; i < 300; i++ {
		if !state.subscriptionLimiter.allow(fmt.Sprintf("prefill-%d", i), "127.0.0.1:1", now) {
			t.Fatalf("prefill %d rejected", i)
		}
	}

	independent := httptest.NewRequest(http.MethodGet, "/s/dummy-token", nil)
	independent.RemoteAddr = "127.0.0.1:54321"
	independent.Header.Set("X-Forwarded-For", "203.0.113.10")
	independentRec := httptest.NewRecorder()
	router.ServeHTTP(independentRec, independent)
	if independentRec.Code == http.StatusTooManyRequests {
		t.Fatalf("forwarded subscription client shared the Caddy loopback source bucket: %s", independentRec.Body.String())
	}

	for i := 0; i < 300; i++ {
		if !state.subscriptionLimiter.allow(fmt.Sprintf("forwarded-%d", i), "203.0.113.50", now) {
			t.Fatalf("forwarded prefill %d rejected", i)
		}
	}
	shared := httptest.NewRequest(http.MethodGet, "/s/dummy-token", nil)
	shared.RemoteAddr = "127.0.0.1:54321"
	shared.Header.Set("X-Forwarded-For", "203.0.113.50")
	sharedRec := httptest.NewRecorder()
	router.ServeHTTP(sharedRec, shared)
	if sharedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("shared forwarded subscription client status=%d body=%s", sharedRec.Code, sharedRec.Body.String())
	}

	login := httptest.NewRecorder()
	router.ServeHTTP(login, caddyLoginRequest("audit-operator", "203.0.113.77"))
	if login.Code != http.StatusUnauthorized {
		t.Fatalf("audit login status=%d body=%s", login.Code, login.Body.String())
	}
	records, err := state.auditRecorder().List(20, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		if record.Action != "auth.login" || record.Actor != "audit-operator" {
			continue
		}
		found = true
		if record.IP != "203.0.113.77" {
			t.Fatalf("audit IP=%q, want forwarded client not loopback", record.IP)
		}
	}
	if !found {
		t.Fatalf("missing auth.login audit record: %+v", records)
	}
}

func TestDirectPanelIgnoresForwardedForOnLoopback(t *testing.T) {
	router, reloader := newTestRouter(ServerInfo{
		Version:     "test",
		Mode:        "server",
		ApplyRoot:   t.TempDir(),
		PanelAccess: "direct",
		PanelListen: "0.0.0.0:2096",
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })

	var last *httptest.ResponseRecorder
	for i := 0; i < 4; i++ {
		last = httptest.NewRecorder()
		req := caddyLoginRequest(fmt.Sprintf("direct-%d", i), fmt.Sprintf("203.0.113.%d", i+1))
		router.ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("untrusted loopback X-Forwarded-For bypassed the shared RemoteAddr bucket: %d %s", last.Code, last.Body.String())
	}
}
