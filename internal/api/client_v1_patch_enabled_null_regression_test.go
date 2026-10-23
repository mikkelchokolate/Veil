package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestV1PatchEnabledNullClearsToDefaultTrue pins #1450: PATCH with an explicit
// "enabled": null clears the field to its documented default (true), the same
// default POST applies to an omitted or null enabled. It must never disable
// the client.
func TestV1PatchEnabledNullClearsToDefaultTrue(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	create := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"enabled-null","enabled":null,"notes":"keep"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	created := unwrapClient(t, create.Body.Bytes())
	id := created["id"].(string)
	if created["enabled"] != true {
		t.Fatalf("create with enabled:null must default to true, got %v", created["enabled"])
	}

	patch := func(body string) map[string]any {
		t.Helper()
		w := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id, body)
		if w.Code != http.StatusOK {
			t.Fatalf("patch %s: %d %s", body, w.Code, w.Body.String())
		}
		var view map[string]any
		if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	stored := func() bool {
		t.Helper()
		c, err := st.clientRepo.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		return c.Enabled
	}

	// Enabled client + null stays enabled (previously flipped to false).
	if view := patch(`{"version":1,"enabled":null}`); view["enabled"] != true {
		t.Fatalf("enabled:null on an enabled client: response enabled=%v", view["enabled"])
	}
	if !stored() {
		t.Fatal("enabled:null on an enabled client persisted enabled=false")
	}

	// Explicit false still disables.
	if view := patch(`{"version":2,"enabled":false}`); view["enabled"] != false {
		t.Fatalf("enabled:false: response enabled=%v", view["enabled"])
	}
	if stored() {
		t.Fatal("enabled:false did not persist")
	}

	// Omitted preserves the disabled state.
	if view := patch(`{"version":3,"notes":"changed"}`); view["enabled"] != false {
		t.Fatalf("omitted enabled must be preserved, got %v", view["enabled"])
	}

	// Null on a disabled client clears back to the default (true).
	view := patch(`{"version":4,"enabled":null}`)
	if view["enabled"] != true {
		t.Fatalf("enabled:null on a disabled client: response enabled=%v", view["enabled"])
	}
	if !stored() {
		t.Fatal("enabled:null on a disabled client did not persist the default true")
	}
	if view["name"] != "enabled-null" || view["notes"] != "changed" {
		t.Fatalf("unrelated fields lost: %+v", view)
	}
}
