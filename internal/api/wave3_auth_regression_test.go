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
	rec := post("correct-password")
	if rec.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertLoginSetsSessionCookieAndCSRF(t, rec)
	state.mu.Lock()
	// The backoff map is keyed by clientIP|username (#667): this request
	// came from 192.0.2.10.
	_, stillBackedOff := state.loginBackoff["192.0.2.10|admin"]
	state.mu.Unlock()
	if stillBackedOff {
		t.Fatal("successful login did not clear backoff")
	}
	if rec := post("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("failure after success should restart backoff, status=%d", rec.Code)
	}
}

// Regression for #667: the per-username login budget and backoff are scoped
// to (clientIP, username). An attacker who knows a username must not be able
// to burn a process-global budget from many IPs and lock out the legitimate
// admin logging in from a different address.
func TestLoginBackoffIsScopedToClientIP(t *testing.T) {
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
	post := func(remoteAddr, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Burn the (attacker-IP, admin) budget: the burst of 3 plus one more
	// trips the username limiter, and every failure also feeds backoff.
	var attackerLast *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		attackerLast = post("198.51.100.10:1000", "wrong")
	}
	if attackerLast.Code != http.StatusTooManyRequests {
		t.Fatalf("attacker should be throttled on their own address, got %d", attackerLast.Code)
	}

	// The victim on a different network must still be able to log in — their
	// (victim-IP, admin) bucket is untouched.
	victim := post("203.0.113.20:2000", "correct-password")
	if victim.Code != http.StatusOK {
		t.Fatalf("victim login from another IP must not inherit the attacker's throttle, status=%d body=%s", victim.Code, victim.Body.String())
	}
	assertLoginSetsSessionCookieAndCSRF(t, victim)

	// And the attacker stays throttled on their own address.
	if rec := post("198.51.100.10:1001", "correct-password"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attacker address must stay throttled, status=%d", rec.Code)
	}
}
