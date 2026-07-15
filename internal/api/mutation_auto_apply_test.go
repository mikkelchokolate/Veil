package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// setupAutoApplyTestHooks swaps the validator and service runner so auto-apply
// can be observed without running real system commands.
func setupAutoApplyTestHooks(t *testing.T) (restore func(), calls *[][]string) {
	t.Helper()
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origAutoApply := autoApplyAfterMutation
	restore = func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		autoApplyAfterMutation = origAutoApply
	}

	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		results := make([]ConfigValidationResult, 0, len(paths))
		for _, path := range paths {
			results = append(results, ConfigValidationResult{Name: path, Config: path, Valid: true})
		}
		return results
	}

	var captured [][]string
	calls = &captured
	serviceActionRunner = func(command []string) ServiceActionResult {
		captured = append(captured, append([]string(nil), command...))
		*calls = captured
		return ServiceActionResult{Command: command, Success: true}
	}
	return restore, calls
}

func hasServiceActionFor(calls [][]string, unit string) bool {
	for _, c := range calls {
		if len(c) >= 3 && c[0] == "systemctl" && c[2] == unit {
			return true
		}
	}
	return false
}

// seedInboundForAutoApplyTests creates a hysteria2 inbound so that subsequent
// mutations have a concrete service action in the auto-apply plan.
func seedInboundForAutoApplyTests(r http.Handler, calls *[][]string) {
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":9443,"enabled":true}`)))
	if calls != nil {
		*calls = (*calls)[:0]
	}
}

func TestSettingsUpdateTriggersAutoApply(t *testing.T) {
	restore, calls := setupAutoApplyTestHooks(t)
	defer restore()

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	seedInboundForAutoApplyTests(r, calls)

	update := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:8080","mode":"dev","fallbackRoot":"/var/lib/veil/www"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, update)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasServiceActionFor(*calls, "veil-hysteria2@hy2.service") {
		t.Fatalf("expected settings update to trigger auto-apply, calls=%v", *calls)
	}
}

func TestRoutingRuleCreateTriggersAutoApply(t *testing.T) {
	restore, calls := setupAutoApplyTestHooks(t)
	defer restore()

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	seedInboundForAutoApplyTests(r, calls)

	create := httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"openai","match":"geosite:openai","outbound":"direct","enabled":true}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, create)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !hasServiceActionFor(*calls, "veil-hysteria2@hy2.service") {
		t.Fatalf("expected routing rule create to trigger auto-apply, calls=%v", *calls)
	}
}

func TestRoutingRuleUpdateTriggersAutoApply(t *testing.T) {
	restore, calls := setupAutoApplyTestHooks(t)
	defer restore()

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	seedInboundForAutoApplyTests(r, calls)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"openai","match":"geosite:openai","outbound":"direct","enabled":true}`)))
	*calls = (*calls)[:0]

	update := httptest.NewRequest(http.MethodPut, "/api/routing/rules/openai", strings.NewReader(`{"match":"geosite:netflix","outbound":"direct","enabled":true}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, update)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasServiceActionFor(*calls, "veil-hysteria2@hy2.service") {
		t.Fatalf("expected routing rule update to trigger auto-apply, calls=%v", *calls)
	}
}

func TestRoutingRuleDeleteTriggersAutoApply(t *testing.T) {
	restore, calls := setupAutoApplyTestHooks(t)
	defer restore()

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	seedInboundForAutoApplyTests(r, calls)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/routing/rules", strings.NewReader(`{"name":"openai","match":"geosite:openai","outbound":"direct","enabled":true}`)))
	*calls = (*calls)[:0]

	del := httptest.NewRequest(http.MethodDelete, "/api/routing/rules/openai", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, del)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if !hasServiceActionFor(*calls, "veil-hysteria2@hy2.service") {
		t.Fatalf("expected routing rule delete to trigger auto-apply, calls=%v", *calls)
	}
}

func TestRoutingPresetApplyTriggersAutoApply(t *testing.T) {
	restore, calls := setupAutoApplyTestHooks(t)
	defer restore()

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})
	seedInboundForAutoApplyTests(r, calls)

	apply := httptest.NewRequest(http.MethodPost, "/api/routing/presets/all", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, apply)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasServiceActionFor(*calls, "veil-hysteria2@hy2.service") {
		t.Fatalf("expected routing preset apply to trigger auto-apply, calls=%v", *calls)
	}
}

func TestWarpUpdateTriggersAutoApply(t *testing.T) {
	restore, calls := setupAutoApplyTestHooks(t)
	defer restore()

	applyRoot := t.TempDir()
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", ApplyRoot: applyRoot})

	body := strings.NewReader(`{"enabled":true,"licenseKey":"","endpoint":"engage.cloudflareclient.com:2408","privateKey":"warp-private-key","localAddress":"172.16.0.2/32","peerPublicKey":"warp-peer-key","socksPort":40000}`)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !hasServiceActionFor(*calls, "veil-warp.service") {
		t.Fatalf("expected WARP update to trigger auto-apply, calls=%v", *calls)
	}
}
