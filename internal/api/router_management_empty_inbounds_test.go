package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFreshManagementPanelStartsWithoutDefaultInbounds(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/inbounds code=%d body=%s", w.Code, w.Body.String())
	}
	var inbounds []Inbound
	if err := json.NewDecoder(w.Body).Decode(&inbounds); err != nil {
		t.Fatalf("decode inbounds: %v", err)
	}
	if len(inbounds) != 0 {
		t.Fatalf("fresh Panel should start without default Inbounds, got %+v", inbounds)
	}
}
