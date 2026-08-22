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
