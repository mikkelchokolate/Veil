package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	veilwarp "github.com/mikkelchokolate/Veil/internal/warp"
)

// TestManagementAPIWarpEnableAutoRegisters verifies the "just flip the toggle"
// behaviour: enabling WARP with no key/license auto-provisions an account,
// persists it, and adds a WARP routing rule. Without auto-registration the PUT
// would fail validation ("WARP private key is required").
func TestManagementAPIWarpEnableAutoRegisters(t *testing.T) {
	orig := warpRegisterFunc
	t.Cleanup(func() { warpRegisterFunc = orig })
	var called bool
	warpRegisterFunc = func(ctx context.Context) (veilwarp.Registration, error) {
		called = true
		return veilwarp.Registration{
			PrivateKey:    "auto-private",
			PeerPublicKey: "auto-peer",
			Endpoint:      "engage.cloudflareclient.com:2408",
			LocalAddress:  "172.16.0.2/32,2606:4700:110::1/128",
			Reserved:      []int{1, 2, 3},
			License:       "auto-license",
		}, nil
	}

	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	// Flip the toggle with nothing else.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("enabling WARP with no key should auto-register and return 200, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("warp registrar was not invoked")
	}
	var resp WarpConfig
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode warp response: %v", err)
	}
	if !resp.Enabled || resp.Endpoint != "engage.cloudflareclient.com:2408" {
		t.Fatalf("auto-registered warp config wrong: %+v", resp)
	}

	// Persisted: a follow-up GET still reports enabled.
	g := httptest.NewRecorder()
	r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/api/warp", nil))
	if !strings.Contains(g.Body.String(), `"enabled":true`) {
		t.Fatalf("WARP enable did not persist: %s", g.Body.String())
	}

	// A WARP routing rule was added automatically.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/routing/rules", nil))
	if !strings.Contains(rr.Body.String(), `"outbound":"warp"`) {
		t.Fatalf("expected an auto-added WARP routing rule: %s", rr.Body.String())
	}
}

// TestManagementAPIWarpReEnableWithRedactedPlaceholderReRegisters guards the
// re-enable path: a stale UI re-sends "[REDACTED]" for the key, but if the
// stored key was cleared (e.g. after a previous disable) resolving the
// placeholder yields an empty key. WARP must re-register rather than enable an
// empty, non-functional config.
func TestManagementAPIWarpReEnableWithRedactedPlaceholderReRegisters(t *testing.T) {
	orig := warpRegisterFunc
	t.Cleanup(func() { warpRegisterFunc = orig })
	var calls int
	warpRegisterFunc = func(ctx context.Context) (veilwarp.Registration, error) {
		calls++
		return veilwarp.Registration{
			PrivateKey:    "auto-private",
			PeerPublicKey: "auto-peer",
			Endpoint:      "engage.cloudflareclient.com:2408",
			LocalAddress:  "172.16.0.2/32,2606:4700:110::1/128",
			Reserved:      []int{1, 2, 3},
			License:       "auto-license",
		}, nil
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	// Enable, then disable clearing the key (a minimal client sends empty).
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true}`)))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":false,"privateKey":"","peerPublicKey":""}`)))

	// Re-enable the way a stale UI does: enabled=true with the redacted sentinel.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true,"privateKey":"[REDACTED]","licenseKey":"[REDACTED]"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("re-enable should auto-register and return 200, got %d: %s", w.Code, w.Body.String())
	}
	if calls != 2 {
		t.Fatalf("expected re-registration on re-enable, registrar called %d time(s)", calls)
	}
	var resp WarpConfig
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode warp response: %v", err)
	}
	if resp.PeerPublicKey != "auto-peer" || resp.LocalAddress == "" {
		t.Fatalf("re-enabled WARP has empty credentials: %+v", resp)
	}
}

func TestManagementAPIWarpEnableSurfacesRegistrationFailure(t *testing.T) {
	orig := warpRegisterFunc
	t.Cleanup(func() { warpRegisterFunc = orig })
	warpRegisterFunc = func(ctx context.Context) (veilwarp.Registration, error) {
		return veilwarp.Registration{}, context.DeadlineExceeded
	}
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(`{"enabled":true}`)))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("registration failure should surface as 502, got %d: %s", w.Code, w.Body.String())
	}
}
