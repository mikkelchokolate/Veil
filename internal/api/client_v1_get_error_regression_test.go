package api

import (
	"net/http"
	"testing"
)

func TestV1GetClientStorageFailureIsNot404(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"get-fail"}`).Body.Bytes())
	id := created["id"].(string)

	missing := v1Request(t, r, http.MethodGet, "/api/v1/clients/missing-client", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing client status=%d body=%s", missing.Code, missing.Body.String())
	}

	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	got := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id, "")
	if got.Code == http.StatusNotFound {
		t.Fatalf("storage failure returned 404: %s", got.Body.String())
	}
	if got.Code < 500 {
		t.Fatalf("storage failure status=%d body=%s, want 5xx", got.Code, got.Body.String())
	}
}
