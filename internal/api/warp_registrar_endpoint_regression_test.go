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
