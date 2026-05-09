package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyPlanRejectsMultipleEnabledInboundsPerProtocol(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	settingsBody := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","email":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret","hysteria2Password":"hy2-secret"}`)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/settings", settingsBody))

	create := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create inbound failed: %d %s", w.Code, w.Body.String())
		}
	}
	create(`{"name":"naive-a","protocol":"naiveproxy","transport":"tcp","port":9443,"enabled":true,"password":"a"}`)
	create(`{"name":"naive-b","protocol":"naiveproxy","transport":"tcp","port":9444,"enabled":true,"password":"b"}`)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/apply/plan", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var plan ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.Valid || !strings.Contains(strings.Join(plan.Errors, "\n"), "multiple enabled naiveproxy inbounds") {
		t.Fatalf("plan should reject multiple enabled naiveproxy inbounds: %+v", plan)
	}
}
