package api

import (
	"net/http"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

func TestV1TrafficTopFailsClosedOnTotalsError(t *testing.T) {
	router, state := newTrafficRouter(t)
	seedTrafficClient(t, router, state, "top-fail", 50, 50)
	dead := testdb.Open(t)
	if err := dead.Close(); err != nil {
		t.Fatal(err)
	}
	state.trafficStore = client.NewTrafficStore(dead)
	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/top", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("top status=%d body=%s, want 500 instead of a truncated 200", w.Code, w.Body.String())
	}
}
