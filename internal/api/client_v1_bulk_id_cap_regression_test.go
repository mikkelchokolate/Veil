package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// bulkIDs returns n client ids: the given real ids first, then distinct
// missing ids up to n.
func bulkIDs(n int, real ...string) []string {
	ids := append([]string(nil), real...)
	for i := len(ids); i < n; i++ {
		ids = append(ids, fmt.Sprintf("missing-%05d", i))
	}
	return ids
}

func postV1Bulk(t *testing.T, r http.Handler, action string, ids []string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"action": action, "clientIds": ids})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients/bulk", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func v1ClientEnabled(t *testing.T, r http.Handler, id string) bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clients/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get client %s: %d %s", id, w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode client: %v", err)
	}
	obj := resp
	if c, ok := resp["client"].(map[string]any); ok {
		obj = c
	}
	enabled, ok := obj["enabled"].(bool)
	if !ok {
		t.Fatalf("client %s has no boolean enabled: %v", id, resp)
	}
	return enabled
}

// TestV1BulkRejectsMoreThanMaxClientIDs pins #1272 item 1: one admin request
// must not run an unbounded per-id savepoint loop under the global client
// mutation lock. A request above the cap is refused before the transaction
// opens, so nothing in it is applied — including the real client listed first.
func TestV1BulkRejectsMoreThanMaxClientIDs(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	id := createV1Client(t, r, "bulk-cap-over")

	w := postV1Bulk(t, r, "disable", bulkIDs(maxV1BulkClientIDs+1, id))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bulk with %d ids: status %d, want 400: %.300s", maxV1BulkClientIDs+1, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), fmt.Sprintf("at most %d", maxV1BulkClientIDs)) {
		t.Errorf("bulk cap error does not name the limit: %s", w.Body.String())
	}
	if !v1ClientEnabled(t, r, id) {
		t.Fatalf("over-cap bulk disable was partially applied: client %s is disabled", id)
	}
}

// TestV1BulkAcceptsExactlyMaxClientIDs is the boundary control: the SPA sends
// the ids selected on one list page, and a page holds up to
// maxV1ClientListPageSize clients, so a full page must still be accepted.
func TestV1BulkAcceptsExactlyMaxClientIDs(t *testing.T) {
	if maxV1BulkClientIDs < maxV1ClientListPageSize {
		t.Fatalf("maxV1BulkClientIDs=%d is below the list page cap %d: a full selected page would be refused", maxV1BulkClientIDs, maxV1ClientListPageSize)
	}
	r, _ := newApplyTrackedRouter(t)
	id := createV1Client(t, r, "bulk-cap-at")

	w := postV1Bulk(t, r, "disable", bulkIDs(maxV1BulkClientIDs, id))
	if w.Code != http.StatusOK {
		t.Fatalf("bulk with exactly %d ids: status %d, want 200: %.300s", maxV1BulkClientIDs, w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["total"] != float64(maxV1BulkClientIDs) || resp["succeeded"] != float64(1) {
		t.Errorf("bulk counters wrong at the cap: total=%v succeeded=%v", resp["total"], resp["succeeded"])
	}
	if v1ClientEnabled(t, r, id) {
		t.Fatalf("bulk disable at the cap did not apply to client %s", id)
	}
}

// TestOpenAPIBulkClientIDsMaxItemsMatchesServerCap keeps the published
// contract (and the generated SDK/zod schemas derived from it) in step with
// the cap the handler enforces.
func TestOpenAPIBulkClientIDsMaxItemsMatchesServerCap(t *testing.T) {
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	node, ok := spec.Paths["/api/v1/clients/bulk"]["post"]
	if !ok {
		t.Fatal("openapi has no POST /api/v1/clients/bulk")
	}
	var op struct {
		RequestBody struct {
			Content map[string]struct {
				Schema struct {
					Properties map[string]struct {
						MaxItems *int `yaml:"maxItems"`
					} `yaml:"properties"`
				} `yaml:"schema"`
			} `yaml:"content"`
		} `yaml:"requestBody"`
		Responses map[string]any `yaml:"responses"`
	}
	if err := node.Decode(&op); err != nil {
		t.Fatalf("decode POST /api/v1/clients/bulk: %v", err)
	}
	got := op.RequestBody.Content["application/json"].Schema.Properties["clientIds"].MaxItems
	if got == nil || *got != maxV1BulkClientIDs {
		t.Fatalf("openapi clientIds maxItems=%v, want %d", got, maxV1BulkClientIDs)
	}
	if _, ok := op.Responses["400"]; !ok {
		t.Fatal("openapi POST /api/v1/clients/bulk does not document the 400 the cap returns")
	}
}
