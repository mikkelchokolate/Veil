package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestV1PatchClearsExhaustedAndScheduledQuota(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients",
		`{"name":"quota-patch","notes":"keep","quotaBytes":1000}`).Body.Bytes())
	id := created["id"].(string)

	current, err := st.clientRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	resetAt := int64(1_700_000_100)
	current.Depleted = true
	current.QuotaResetPolicy = client.ResetDaily
	current.QuotaResetAt = &resetAt
	if _, err := st.clientRepo.Update(current, current.Version); err != nil {
		t.Fatal(err)
	}

	stale := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id, `{"version":1,"quotaBytes":null}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale version status=%d body=%s", stale.Code, stale.Body.String())
	}

	w := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id, `{"version":2,"quotaBytes":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear quota: %d %s", w.Code, w.Body.String())
	}
	var view map[string]any
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view["quotaBytes"] != nil {
		t.Fatalf("quotaBytes=%v", view["quotaBytes"])
	}
	if view["depleted"] != false {
		t.Fatalf("depleted=%v", view["depleted"])
	}
	if view["quotaResetAt"] != nil {
		t.Fatalf("quotaResetAt=%v", view["quotaResetAt"])
	}
	if view["quotaResetPolicy"] != client.ResetNever {
		t.Fatalf("quotaResetPolicy=%v", view["quotaResetPolicy"])
	}
	if view["notes"] != "keep" || view["name"] != "quota-patch" {
		t.Fatalf("unrelated fields lost: %+v", view)
	}
}
