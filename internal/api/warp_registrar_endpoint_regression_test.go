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

// #1292 F1: the endpoint returned by WARP auto-registration is advisory. It
// may fill an unset or stock endpoint, but must not silently override an
// endpoint the operator submitted in the same PUT /api/warp.
func TestManagementAPIWarpRegistrarEndpointDoesNotOverrideOperatorEndpoint(t *testing.T) {
	orig := warpRegisterFunc
	t.Cleanup(func() { warpRegisterFunc = orig })
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	t.Cleanup(func() { autoApplyAfterMutation = origAutoApply })
	warpRegisterFunc = func(ctx context.Context) (veilwarp.Registration, error) {
		return veilwarp.Registration{
			PrivateKey:    "auto-private",
			PeerPublicKey: "auto-peer",
			Endpoint:      "engage-alt.cloudflareclient.com:2408",
			LocalAddress:  "172.16.0.2/32",
			Reserved:      []int{1, 2, 3},
		}, nil
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{"operator endpoint wins", `{"enabled":true,"endpoint":"162.159.192.1:2408"}`, "162.159.192.1:2408"},
		{"unset endpoint takes registrar", `{"enabled":true}`, "engage-alt.cloudflareclient.com:2408"},
		{"stock endpoint takes registrar", `{"enabled":true,"endpoint":"` + veilwarp.DefaultEndpoint + `"}`, "engage-alt.cloudflareclient.com:2408"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(tc.body)))
			if w.Code != http.StatusOK {
				t.Fatalf("PUT /api/warp = %d: %s", w.Code, w.Body.String())
			}
			var resp WarpConfig
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Endpoint != tc.want {
				t.Fatalf("endpoint = %q, want %q", resp.Endpoint, tc.want)
			}
		})
	}
}

// A stored custom endpoint survives a later partial PUT that omits the
// endpoint, both without and with auto-registration (review on #1364).
func TestManagementAPIWarpPartialPutKeepsStoredEndpoint(t *testing.T) {
	orig := warpRegisterFunc
	t.Cleanup(func() { warpRegisterFunc = orig })
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	t.Cleanup(func() { autoApplyAfterMutation = origAutoApply })
	var calls int
	warpRegisterFunc = func(ctx context.Context) (veilwarp.Registration, error) {
		calls++
		return veilwarp.Registration{
			PrivateKey:    "auto-private",
			PeerPublicKey: "auto-peer",
			Endpoint:      "engage-alt.cloudflareclient.com:2408",
			LocalAddress:  "172.16.0.2/32",
			Reserved:      []int{1, 2, 3},
		}, nil
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
	put := func(body string) WarpConfig {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s = %d: %s", body, w.Code, w.Body.String())
		}
		var resp WarpConfig
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}
	const custom = "162.159.192.1:2408"
	if got := put(`{"enabled":true,"endpoint":"` + custom + `"}`).Endpoint; got != custom {
		t.Fatalf("initial endpoint = %q, want %q", got, custom)
	}
	// Disable, clearing the key so the next enable must re-register.
	if got := put(`{"enabled":false,"privateKey":"","peerPublicKey":""}`).Endpoint; got != custom {
		t.Fatalf("partial disable PUT reset endpoint to %q, want %q", got, custom)
	}
	if got := put(`{"enabled":true}`).Endpoint; got != custom {
		t.Fatalf("re-register replaced stored endpoint with %q, want %q", got, custom)
	}
	if calls != 2 {
		t.Fatalf("registrar called %d times, want 2", calls)
	}
	// An explicit empty endpoint (the panel's cleared input) still resets
	// to the stock endpoint (a disable does not register, so the registrar
	// cannot fill it).
	if got := put(`{"enabled":false,"endpoint":""}`).Endpoint; got != veilwarp.DefaultEndpoint {
		t.Fatalf("explicit empty endpoint = %q, want %q", got, veilwarp.DefaultEndpoint)
	}
}
