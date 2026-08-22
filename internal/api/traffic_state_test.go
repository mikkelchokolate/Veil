package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestV1TrafficReportsProviderState asserts (A9) the traffic endpoints report
// an honest telemetry state (no providers / collecting) instead of silently
// returning zeros that look like real data.
func TestV1TrafficReportsProviderState(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// /api/v1/traffic/summary must carry a provider state so the UI can render
	// honest "no traffic source" instead of a fake 90-day zero graph.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/summary", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatalf("summary endpoint missing (A9)")
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	state, _ := resp["state"].(string)
	if state == "" {
		t.Errorf("summary missing telemetry state: %v", resp)
	}
	// With no providers registered the state must be the honest
	// "unsupported"/"no_providers" marker, not "collecting".
	if state == "collecting" {
		t.Errorf("state=collecting with zero providers registered (dishonest)")
	}
}
