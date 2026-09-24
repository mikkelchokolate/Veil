package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

func TestV1ClientAuditIgnoresUnrelatedNameTargets(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"demo"}`).Body.Bytes())
	id := created["id"].(string)

	if err := st.auditRecorder().Append(audit.Record{
		Timestamp: time.Now().Add(-time.Minute),
		Action:    "create_inbound",
		Target:    "demo",
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.auditRecorder().Append(audit.Record{
		Timestamp: time.Now(),
		Action:    "create_client",
		Target:    "demo",
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}

	w := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id+"/audit", "")
	if w.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []struct {
			Action string `json:"action"`
			Target string `json:"target"`
		} `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// Negative-only filtering greens a broken matcher returning [] — the
	// client event targeted at "demo" must be positively present (#874).
	foundClientEvent := false
	for _, item := range body.Items {
		if item.Action == "create_inbound" {
			t.Fatalf("inbound event leaked into client audit: %+v", item)
		}
		if item.Action == "create_client" && item.Target == "demo" {
			foundClientEvent = true
		}
	}
	if len(body.Items) == 0 || !foundClientEvent {
		t.Fatalf("client audit lost its own event (items=%v)", body.Items)
	}

	rename := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id, `{"version":1,"name":"renamed"}`)
	if rename.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rename.Code, rename.Body.String())
	}
	after := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id+"/audit", "")
	if err := json.NewDecoder(after.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	foundClientEvent = false
	for _, item := range body.Items {
		if item.Action == "create_inbound" {
			t.Fatalf("rename pulled inbound history into client audit: %+v", item)
		}
		// Real client audit events are recorded against the client id, which
		// survives the rename; a positive match proves the matcher still
		// returns this client's own history rather than an empty page.
		if item.Target == id {
			foundClientEvent = true
		}
	}
	if len(body.Items) == 0 || !foundClientEvent {
		t.Fatalf("client audit after rename lost its own event (items=%v)", body.Items)
	}
}

func TestV1ClientAuditPagesPastUnrelatedGlobalEvents(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	created := unwrapClient(t, v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"audit-old"}`).Body.Bytes())
	id := created["id"].(string)

	old := time.Now().Add(-time.Hour)
	if err := st.auditRecorder().Append(audit.Record{
		Timestamp: old,
		Action:    "update_client",
		Target:    id,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if err := st.auditRecorder().Append(audit.Record{
			Timestamp: old.Add(time.Duration(i+1) * time.Second),
			Action:    "update_client",
			Target:    fmt.Sprintf("other-%d", i),
			Success:   true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	w := v1Request(t, r, http.MethodGet, "/api/v1/clients/"+id+"/audit?limit=10", "")
	if w.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []struct {
			Target string `json:"target"`
		} `json:"items"`
		NextBefore string `json:"nextBefore"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) == 0 {
		t.Fatal("older client event was hidden by 500 newer unrelated records")
	}
	found := false
	for _, item := range body.Items {
		if item.Target == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("client event missing: %+v", body.Items)
	}
}
