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
	if w.Code != http.StatusOK {
		t.Fatalf("summary endpoint must return 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode summary body: %v: %s", err, w.Body.String())
	}
	// With no providers registered the state must be exactly the honest
	// "unsupported" marker — "collecting", "pending", "" or any other state
	// would lie about telemetry that does not exist (A9).
	if state, _ := resp["state"].(string); state != "unsupported" {
		t.Errorf("summary state=%q want %q with zero providers registered: %v", resp["state"], "unsupported", resp)
	}
}
