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

	inboundBody := strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true}`)
	iw := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/inbounds", inboundBody)
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(iw, ireq)

	id := createV1ClientWithBinding(t, r, "bulk-c", "hy2", "p")

	body := strings.NewReader(`{"action":"reset_traffic","clientIds":["` + id + `","missing-id"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients/bulk", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}
	raw := w.Body.Bytes()
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Honest mutation envelope: success true (one client applied), a real
	// revision, and an applyJob pinned to that exact revision.
	env := decodeEnvelope(t, raw)
	if !env.Success {
		t.Errorf("bulk success=false when a client was applied: %s", w.Body.String())
	}
	if env.Revision.Desired < 1 {
		t.Errorf("bulk revision.desired=%d, want >=1", env.Revision.Desired)
	}
	if env.ApplyJob == nil || env.ApplyJob.DesiredRevision != env.Revision.Desired {
		t.Errorf("bulk applyJob missing or pinned to wrong revision: %+v (desired=%d)", env.ApplyJob, env.Revision.Desired)
	}
	// Exact per-client accounting: one applied, one bad id failed, none skipped.
	if resp["succeeded"] != float64(1) || resp["failed"] != float64(1) || resp["skipped"] != float64(0) || resp["total"] != float64(2) {
		t.Errorf("bulk counters wrong: %v", resp)
	}
}
