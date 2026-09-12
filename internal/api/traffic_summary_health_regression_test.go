package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestTrafficSummaryIsPendingBeforeFirstSuccessfulObservation(t *testing.T) {
	state := newClientLifecycleTestState(t)
	provider := &healthRegressionTrafficProvider{key: "hysteria2:new"}
	if err := state.trafficCollector.ResetProductionProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/summary", nil)
	rec := httptest.NewRecorder()
	state.handleV1TrafficSummary(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["state"] == "healthy" {
		t.Fatalf("summary reported healthy before any observation: %#v", body)
	}
}
