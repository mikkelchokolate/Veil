package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementAPIWarpPutRejectsInvalidNumericValues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "high socks port", body: `{"enabled":false,"socksPort":65536}`},
		{name: "low mtu", body: `{"enabled":false,"mtu":575}`},
		{name: "reserved length", body: `{"enabled":false,"reserved":[1,2]}`},
		{name: "reserved range", body: `{"enabled":false,"reserved":[1,256,3]}`},
		// #1160: socksListen must be an IPv4 literal inside 127.41.0.0/16 —
		// non-IP and non-loopback input is rejected loudly, never rewritten.
		{name: "non-loopback socks listen", body: `{"enabled":false,"socksListen":"192.168.1.10"}`},
		{name: "unspecified socks listen", body: `{"enabled":false,"socksListen":"0.0.0.0"}`},
		{name: "hostname socks listen", body: `{"enabled":false,"socksListen":"localhost"}`},
		{name: "mapped socks listen", body: `{"enabled":false,"socksListen":"::ffff:127.41.0.1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/api/warp", strings.NewReader(tt.body))
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// #1160: a loopback socksListen outside the egress-pierced 127.41/16 band —
// including the pre-#1097 default 127.0.0.1 that GET echoes back from
// persisted state — is migrated to the band default rather than rejected, so
// re-submitting a stale config keeps working.
func TestManagementAPIWarpPutMigratesOutOfBandLoopbackSocksListen(t *testing.T) {
	for _, listen := range []string{"127.0.0.1", "127.0.0.5", "::1"} {
		t.Run(listen, func(t *testing.T) {
			router, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev"})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/api/warp",
				strings.NewReader(`{"enabled":false,"socksListen":"`+listen+`"}`))
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"socksListen":"127.41.0.1"`) {
				t.Fatalf("socksListen %q must persist as the band default: %s", listen, recorder.Body.String())
			}
		})
	}
}
