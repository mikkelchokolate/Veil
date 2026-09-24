package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// naiveproxy permits multiple enabled inbounds (MaxEnabled()==0): they all
// consolidate into the single Caddy JSON config served by veil-caddy.service.
// The plan must be valid AND consolidate — exactly one caddy config artifact
// and one consolidated reload, not per-inbound runtime work (#921).
func TestApplyPlanConsolidatesMultipleEnabledNaiveproxyInbounds(t *testing.T) {
	// Anchor the state's live root at the same VEIL_LIVE_ROOT the
	// render-time fallbackRoot default resolves from (hostenv.EtcDir/www),
	// so the render base and the effective root agree (issue #636).
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", LiveRoot: os.Getenv("VEIL_LIVE_ROOT")})
	settingsBody := strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","defaultAcmeEmail":"admin@example.com","naiveUsername":"veil","naivePassword":"naive-secret","hysteria2Password":"hy2-secret"}`)
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
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var plan ApplyPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if !plan.Valid || len(plan.Errors) > 0 {
		t.Fatalf("plan should be valid for multiple enabled naiveproxy inbounds: %+v", plan)
	}

	// Consolidated runtime leg: exactly one caddy config promoted and one
	// consolidated reload of veil-caddy.service — no per-inbound units.
	var caddyConfigs, caddyReloads int
	for _, op := range plan.Operations {
		switch {
		case op.Type == "promote_file" && strings.HasSuffix(op.Destination, "caddy/config.json"):
			caddyConfigs++
		case op.Type == "reload_service" && op.Unit == "veil-caddy.service":
			caddyReloads++
		case op.Type == "restart_service" || strings.Contains(op.Unit, "naive"):
			t.Fatalf("unexpected per-inbound/restart operation for consolidated naive runtime: %+v", op)
		}
	}
	if caddyConfigs != 1 || caddyReloads != 1 {
		t.Fatalf("plan must consolidate naiveproxy into one caddy config + one reload: ops=%+v", plan.Operations)
	}
	if !containsApplyPlanString(plan.Actions, "reload veil-caddy.service") {
		t.Fatalf("plan actions missing consolidated caddy reload: %+v", plan.Actions)
	}
}
