package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestFallbackPasswordIsNotComparedWithBranchingStringEquality(t *testing.T) {
	body, err := os.ReadFile("auth_session.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "req.Password == fallbackPassword") {
		t.Fatal("fallback password uses timing-variable string equality")
	}
}

func TestFailedLoginGetsProgressiveBackoff(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		sessions: registry,
		settings: model.Settings{NaivePassword: "correct-password"},
	}
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		state.handleLogin(rec, req)
		return rec
	}
	first := request()
	if first.Code != http.StatusUnauthorized || first.Header().Get("Retry-After") == "" {
		t.Fatalf("first failure status=%d Retry-After=%q", first.Code, first.Header().Get("Retry-After"))
	}
	second := request()
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("immediate retry status=%d Retry-After=%q", second.Code, second.Header().Get("Retry-After"))
	}
}
