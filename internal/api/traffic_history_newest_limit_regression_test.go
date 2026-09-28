package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestV1TrafficHistoryDefaultLimitKeepsNewestBuckets(t *testing.T) {
	router, state := newTrafficRouter(t)
	id := seedTrafficClient(t, router, state, "histlimit", 1, 0)
	var bindingID string
	if err := state.db.QueryRow(`SELECT id FROM client_bindings WHERE client_id=?`, id).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	// Buckets must sit inside the retention window: the reconciler prunes
	// traffic_samples older than the ~45-day floor on its first pass, which
	// raced epoch-0 timestamps here and flaked the count.
	const buckets int64 = 600
	base := time.Now().Unix() - buckets*60
	base -= base % 60
	for i := int64(1); i < buckets; i++ {
		if err := state.trafficStore.RecordSample(client.Sample{BindingID: bindingID, UploadBytes: 1, AtUnix: base + i*60}); err != nil {
			t.Fatal(err)
		}
	}
	newest := base + (buckets-1)*60
	oldestKept := base + (buckets-500)*60
	w := v1Request(t, router, http.MethodGet, "/api/v1/traffic/"+id+"/history?from=0&to="+strconv.FormatInt(newest, 10)+"&limit=500", "")
	if w.Code != http.StatusOK {
		t.Fatalf("history: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []struct {
			BucketStart int64 `json:"bucketStart"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 500 || len(body.Items) != 500 {
		t.Fatalf("count=%d items=%d, want 500", body.Count, len(body.Items))
	}
	if body.Items[len(body.Items)-1].BucketStart != newest {
		t.Fatalf("newest bucket=%d, want %d (ASC LIMIT dropped current usage)", body.Items[len(body.Items)-1].BucketStart, newest)
	}
	if body.Items[0].BucketStart != oldestKept {
		t.Fatalf("oldest kept bucket=%d, want %d (the newest-500 window)", body.Items[0].BucketStart, oldestKept)
	}
}
