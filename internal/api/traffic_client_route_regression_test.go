package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestTrafficUnknownNestedPathIsNotClientTotals(t *testing.T) {
	state := newClientLifecycleTestState(t)
	row, err := state.clientRepo.Create(client.Client{Name: "route", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/"+row.ID+"/nope", nil)
	rec := httptest.NewRecorder()
	state.handleV1TrafficClient(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown nested path status=%d body=%s", rec.Code, rec.Body.String())
	}
}
