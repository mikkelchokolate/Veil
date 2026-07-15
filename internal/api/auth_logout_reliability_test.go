package api

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestRegisteredLogoutReadsPanelAccessUnderStateLock(t *testing.T) {
	state := &managementState{settings: Settings{PanelAccess: "local"}}
	mux := http.NewServeMux()
	state.register(mux)

	started := make(chan struct{})
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		close(started)
		iteration := 0
		for {
			select {
			case <-stop:
				return
			default:
				state.mu.Lock()
				if iteration%2 == 0 {
					state.settings.PanelAccess = "caddy"
				} else {
					state.settings.PanelAccess = "local"
				}
				state.mu.Unlock()
				iteration++
				runtime.Gosched()
			}
		}
	}()
	<-started

	const requests = 250
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			close(stop)
			writer.Wait()
			t.Fatalf("logout status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	close(stop)
	writer.Wait()
}

func TestLogoutSnapshotKeepsCaddyCookieSecure(t *testing.T) {
	state := &managementState{settings: Settings{PanelAccess: "caddy"}}
	mux := http.NewServeMux()
	state.register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "veil_session=") || !strings.Contains(cookie, "Secure") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("unexpected logout cookie: %q", cookie)
	}
}
