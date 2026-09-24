package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV1BulkReturnsRevisionAndSkipped asserts (A8) the bulk response carries
// the mutation envelope (revision + applyJob) and a per-action skipped count,
// and that "reset_traffic" (clearing actual usage) replaces the misleading
// "reset_quota" that only cleared the depleted flag.
func TestV1BulkReturnsRevisionAndSkipped(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// An unbound client keeps the apply trivially fast and deterministic: the
	// envelope contract under test does not depend on any inbound provider.
	id := createV1Client(t, r, "bulk-c")

	body := strings.NewReader(`{"action":"reset_traffic","clientIds":["` + id + `","missing-id"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients/bulk", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// mutation envelope present — ClientBulkResponse is allOf MutationOutcome,
	// so success is required and reflects the apply outcome, not per-item
	// failures (#772).
	if _, ok := resp["revision"]; !ok {
		t.Errorf("bulk missing revision envelope: %v", keysOf(resp))
	}
	if resp["success"] != true {
		t.Errorf("bulk success = %v, want true (apply outcome, not per-item): %v", resp["success"], resp)
	}
	if _, ok := resp["applyJob"]; !ok {
		t.Errorf("bulk missing applyJob: %v", keysOf(resp))
	}
	// skipped/failed accounting
	if _, ok := resp["skipped"]; !ok {
		t.Errorf("bulk missing skipped count: %v", resp)
	}
	// one bad id must be counted as failed, not crash the batch
	failed, _ := resp["failed"].(float64)
	if failed < 1 {
		t.Errorf("expected >=1 failed for missing-id, got %v", failed)
	}
}
