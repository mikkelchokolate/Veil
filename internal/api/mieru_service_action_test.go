package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceActionRouteAllowsMieruRestart(t *testing.T) {
	oldRunner := serviceControlRunner
	serviceControlRunner = func(name, action string) ServiceActionResponse {
		return ServiceActionResponse{Service: name, Action: action, Success: true}
	}
	t.Cleanup(func() { serviceControlRunner = oldRunner })

	r, _ := NewRouter(ServerInfo{Version: "test"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/services/mieru/restart", strings.NewReader(`{"confirm":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"service":"mieru"`) {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}
