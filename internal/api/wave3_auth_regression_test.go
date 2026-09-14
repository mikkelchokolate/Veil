package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestFallbackPasswordIsNotComparedWithBranchingStringEquality(t *testing.T) {
	for _, name := range []string{"auth_session.go", "auth_login_reliability.go"} {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		if strings.Contains(src, "req.Password == fallbackPassword") ||
			strings.Contains(src, "password == snapshot.FallbackPassword") ||
			strings.Contains(src, "req.Password == snapshot.FallbackPassword") {
			t.Fatalf("%s: fallback password uses timing-variable string equality", name)
		}
	}
}

func registeredLoginMux(state *managementState) *http.ServeMux {
	mux := http.NewServeMux()
	state.register(mux)
	return mux
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
	mux := registeredLoginMux(state)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
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

func TestSuccessfulLoginClearsProgressiveBackoff(t *testing.T) {
	registry, err := NewSessionRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	state := &managementState{
		sessions:        registry,
		settings:        model.Settings{NaivePassword: "correct-password"},
		loginBackoffNow: func() time.Time { return now },
	}
	mux := registeredLoginMux(state)
	post := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:1234"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first failure status=%d", rec.Code)
	}
	now = now.Add(2 * time.Second)
	if rec := post("correct-password"); rec.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", rec.Code, rec.Body.String())
	}
	state.mu.Lock()
	_, stillBackedOff := state.loginBackoff["admin"]
	state.mu.Unlock()
	if stillBackedOff {
		t.Fatal("successful login did not clear backoff")
	}
	if rec := post("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("failure after success should restart backoff, status=%d", rec.Code)
	}
}
