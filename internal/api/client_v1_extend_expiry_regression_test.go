package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestV1BulkExtendExpiryRejectsOverflowDays(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"extend-overflow"}`).Body.Bytes())
	id := created["id"].(string)

	overflowDays := math.MaxInt64/86400 + 1
	body := fmt.Sprintf(`{"action":"extend_expiry","days":%d,"clientIds":[%q]}`, overflowDays, id)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients/bulk", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("overflow days status=%d body=%s", w.Code, w.Body.String())
	}

	got := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id, "")
	var view map[string]any
	if err := json.NewDecoder(got.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view["expiresAt"] != nil {
		t.Fatalf("overflow extend persisted expiresAt=%v", view["expiresAt"])
	}
}

func TestV1BulkExtendExpiryRejectsCapPlusOneAndAdditionOverflow(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"extend-cap"}`).Body.Bytes())
	id := created["id"].(string)

	w := v1Request(t, r, http.MethodPost, "/api/v1/clients/bulk",
		fmt.Sprintf(`{"action":"extend","days":%d,"clientIds":[%q]}`, client.MaxExpiryExtensionDays+1, id))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cap+1 status=%d body=%s", w.Code, w.Body.String())
	}

	ok := v1Request(t, r, http.MethodPost, "/api/v1/clients/bulk",
		fmt.Sprintf(`{"action":"extend","days":%d,"clientIds":[%q]}`, client.MaxExpiryExtensionDays, id))
	if ok.Code != http.StatusOK {
		t.Fatalf("max days status=%d body=%s", ok.Code, ok.Body.String())
	}

	current, err := st.clientRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	nearLimit := int64(math.MaxInt64 - 10)
	current.ExpiresAt = &nearLimit
	if _, err := st.clientRepo.Update(current, current.Version); err != nil {
		t.Fatal(err)
	}

	addOverflow := v1Request(t, r, http.MethodPost, "/api/v1/clients/bulk",
		fmt.Sprintf(`{"action":"extend","days":1,"clientIds":[%q]}`, id))
	if addOverflow.Code != http.StatusOK {
		t.Fatalf("addition overflow bulk status=%d body=%s", addOverflow.Code, addOverflow.Body.String())
	}
	var resp struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	}
	if err := json.NewDecoder(addOverflow.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Succeeded != 0 || resp.Failed != 1 {
		t.Fatalf("addition overflow accounting=%+v", resp)
	}
	after, err := st.clientRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if after.ExpiresAt == nil || *after.ExpiresAt != nearLimit {
		t.Fatalf("addition overflow mutated expiresAt=%v", after.ExpiresAt)
	}
}
