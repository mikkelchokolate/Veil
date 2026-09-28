package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// trafficAllHistoryBody mirrors the {items,count} envelope of the per-client
// history endpoint.
type trafficAllHistoryBody struct {
	Items []struct {
		BucketStart   int64  `json:"bucketStart"`
		ClientID      string `json:"clientId"`
		BindingID     string `json:"bindingId"`
		UploadDelta   int64  `json:"uploadDelta"`
		DownloadDelta int64  `json:"downloadDelta"`
	} `json:"items"`
	Count int `json:"count"`
}

func bindingIDForClient(t *testing.T, state *managementState, clientID string) string {
	t.Helper()
	var bindingID string
	if err := state.db.QueryRow(`SELECT id FROM client_bindings WHERE client_id=? LIMIT 1`, clientID).Scan(&bindingID); err != nil {
		t.Fatalf("binding for %s: %v", clientID, err)
	}
	return bindingID
}

// TestV1TrafficAllHistoryAggregatesAcrossClients: GET /api/v1/traffic/history
// sums every client's samples into one row per bucket with empty
// clientId/bindingId.
func TestV1TrafficAllHistoryAggregatesAcrossClients(t *testing.T) {
	router, state := newTrafficRouter(t)
	first := seedTrafficClient(t, router, state, "allhist-a", 1, 1)
	second := seedTrafficClient(t, router, state, "allhist-b", 2, 2)

	// Buckets must sit inside the retention window: the reconciler prunes
	// traffic_samples older than ~45 days on its first pass, so distant-past
	// timestamps race the pruner. Half an hour back is far inside retention
	// and still outside the seeded samples' current-minute bucket.
	bucket := time.Now().Add(-30 * time.Minute).Unix()
	bucket -= bucket % 60
	for _, sm := range []client.Sample{
		{BindingID: bindingIDForClient(t, state, first), UploadBytes: 10, DownloadBytes: 100, AtUnix: bucket + 10},
		{BindingID: bindingIDForClient(t, state, second), UploadBytes: 20, DownloadBytes: 200, AtUnix: bucket + 40},
		{BindingID: bindingIDForClient(t, state, first), UploadBytes: 1, DownloadBytes: 2, AtUnix: bucket + 60},
	} {
		if err := state.trafficStore.RecordSample(sm); err != nil {
			t.Fatal(err)
		}
	}

	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/history?from="+strconv.FormatInt(bucket, 10)+"&to="+strconv.FormatInt(bucket+60, 10), "")
	if w.Code != http.StatusOK {
		t.Fatalf("aggregate history: %d %s", w.Code, w.Body.String())
	}
	var body trafficAllHistoryBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 2 || len(body.Items) != 2 {
		t.Fatalf("count=%d items=%d, want 2", body.Count, len(body.Items))
	}
	if body.Items[0].BucketStart != bucket || body.Items[0].UploadDelta != 30 || body.Items[0].DownloadDelta != 300 {
		t.Fatalf("bucket %d = %+v, want summed 30/300", bucket, body.Items[0])
	}
	if body.Items[1].BucketStart != bucket+60 || body.Items[1].UploadDelta != 1 || body.Items[1].DownloadDelta != 2 {
		t.Fatalf("bucket %d = %+v, want 1/2", bucket+60, body.Items[1])
	}
	for i, item := range body.Items {
		if item.ClientID != "" || item.BindingID != "" {
			t.Fatalf("aggregate row %d carries attribution %q/%q", i, item.ClientID, item.BindingID)
		}
	}
}

// TestV1TrafficAllHistoryWinsOverClientID pins the dispatch order: "history"
// parses as a valid {clientId} segment, so the static route must claim the
// path even when a client literally has id "history" (impossible via the
// API's uuid ids, but the subtree handler must not depend on that).
func TestV1TrafficAllHistoryWinsOverClientID(t *testing.T) {
	router, state := newTrafficRouter(t)
	if _, err := state.db.Exec(`INSERT INTO clients (id, name, enabled, quota_reset_policy, created_at, updated_at)
	  VALUES ('history','history',1,'never',1,1)`); err != nil {
		t.Fatalf("insert shadow client: %v", err)
	}
	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("aggregate history: %d %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["count"]; !ok {
		t.Fatalf("aggregate shape missing count: %s", w.Body.String())
	}
	// The client-totals shape would carry a top-level clientId echoing
	// "history"; its presence means the wildcard path won the dispatch.
	if _, ok := raw["clientId"]; ok {
		t.Fatalf("request was served as client totals for id 'history': %s", w.Body.String())
	}

	// Stragglers that miss the exact pattern (trailing slash) still reach the
	// subtree handler — it must dispatch them to the aggregate too, before
	// treating the segment as a client id.
	w = v1Request(t, router, http.MethodGet, "/api/v1/traffic/history/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("trailing-slash aggregate history: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["count"]; !ok {
		t.Fatalf("trailing-slash request not dispatched to aggregate: %s", w.Body.String())
	}
}

// TestV1TrafficClientHistoryStillWorks guards the sibling route: adding the
// static /history segment must not break /api/v1/traffic/{id}/history.
func TestV1TrafficClientHistoryStillWorks(t *testing.T) {
	router, state := newTrafficRouter(t)
	id := seedTrafficClient(t, router, state, "stillhist", 7, 9)
	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/"+id+"/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("per-client history: %d %s", w.Code, w.Body.String())
	}
	var body trafficAllHistoryBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count < 1 {
		t.Fatalf("per-client history count=%d, want >=1", body.Count)
	}
	if body.Items[0].ClientID != id {
		t.Fatalf("per-client history row clientId=%q, want %q", body.Items[0].ClientID, id)
	}
}

// TestV1TrafficAllHistoryLimitKeepsNewestBuckets pins the API contract: the
// limit keeps the newest buckets, returned oldest-first — matching the
// per-client history regression test.
func TestV1TrafficAllHistoryLimitKeepsNewestBuckets(t *testing.T) {
	router, state := newTrafficRouter(t)
	id := seedTrafficClient(t, router, state, "allhistlimit", 1, 0)
	bindingID := bindingIDForClient(t, state, id)
	// Recent buckets stay inside the retention window — the reconciler's
	// first-pass pruner deletes anything older than ~45 days.
	const buckets int64 = 10
	newest := time.Now().Unix()
	newest -= newest % 60
	for i := int64(1); i <= buckets; i++ {
		if err := state.trafficStore.RecordSample(client.Sample{BindingID: bindingID, UploadBytes: 1, AtUnix: newest - (buckets-i)*60}); err != nil {
			t.Fatal(err)
		}
	}
	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/history?from=0&to="+strconv.FormatInt(newest, 10)+"&limit=3", "")
	if w.Code != http.StatusOK {
		t.Fatalf("aggregate history: %d %s", w.Code, w.Body.String())
	}
	var body trafficAllHistoryBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 3 || len(body.Items) != 3 {
		t.Fatalf("count=%d items=%d, want 3", body.Count, len(body.Items))
	}
	if body.Items[0].BucketStart != newest-2*60 || body.Items[2].BucketStart != newest {
		t.Fatalf("limit window [%d,%d], want newest 3 ending at %d",
			body.Items[0].BucketStart, body.Items[2].BucketStart, newest)
	}
}

// TestV1TrafficAllHistoryEmpty: no samples -> 200 with an empty items list
// (count 0), never an error — the Traffic page charts "no data".
func TestV1TrafficAllHistoryEmpty(t *testing.T) {
	router, _ := newTrafficRouter(t)
	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("aggregate history on empty store: %d %s", w.Code, w.Body.String())
	}
	var body trafficAllHistoryBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 0 || len(body.Items) != 0 {
		t.Fatalf("empty store count=%d items=%d, want 0", body.Count, len(body.Items))
	}
}

// TestV1TrafficAllHistoryStoreUnavailable: a nil store is 503, both on the
// exact route and on subtree paths that dispatch to the aggregate handler.
func TestV1TrafficAllHistoryStoreUnavailable(t *testing.T) {
	state := &managementState{}
	for _, path := range []string{"/api/v1/traffic/history", "/api/v1/traffic/history/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		if path == "/api/v1/traffic/history" {
			state.handleV1TrafficAllHistory(rec, req)
		} else {
			state.handleV1TrafficClient(rec, req)
		}
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status=%d want 503", path, rec.Code)
		}
	}
}

// TestV1TrafficAllHistoryMethodNotAllowed: only GET is served; the handler —
// not just the auth policy — must reject mutations with 405+Allow.
func TestV1TrafficAllHistoryMethodNotAllowed(t *testing.T) {
	router, _ := newTrafficRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/traffic/history", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST aggregate history: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow header=%q, want GET", rec.Header().Get("Allow"))
	}
}
