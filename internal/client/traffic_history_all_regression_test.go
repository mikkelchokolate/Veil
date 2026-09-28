package client

import (
	"testing"
	"time"
)

// TestHistoryForAllAggregatesClientsAndBindings pins the cross-client,
// cross-binding per-bucket summation behind GET /api/v1/traffic/history:
// every traffic_samples row in a bucket contributes exactly once, and the
// aggregate rows carry no client/binding attribution.
func TestHistoryForAllAggregatesClientsAndBindings(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)

	first, err := repo.Create(Client{Name: "agg-a", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(Client{Name: "agg-b", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	bindA1, err := repo.CreateBinding(Binding{ClientID: first.ID, InboundID: "in-a1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	bindA2, err := repo.CreateBinding(Binding{ClientID: first.ID, InboundID: "in-a2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	bindB1, err := repo.CreateBinding(Binding{ClientID: second.ID, InboundID: "in-b1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// Aligned minute bucket; +10/+20/+40 land inside it, +60 is the next.
	bucket := int64(1_700_000_000)
	bucket -= bucket % 60
	samples := []Sample{
		{BindingID: bindA1.ID, UploadBytes: 10, DownloadBytes: 100, AtUnix: bucket + 10},
		{BindingID: bindA2.ID, UploadBytes: 20, DownloadBytes: 200, AtUnix: bucket + 40},
		{BindingID: bindB1.ID, UploadBytes: 40, DownloadBytes: 400, AtUnix: bucket + 20},
		{BindingID: bindA1.ID, UploadBytes: 1, DownloadBytes: 2, AtUnix: bucket + 60},
		// Outside the queried window: proves from/to actually filter.
		{BindingID: bindB1.ID, UploadBytes: 5, DownloadBytes: 6, AtUnix: bucket - 60},
	}
	for _, sm := range samples {
		if err := store.RecordSample(sm); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.HistoryForAll(bucket, bucket+60, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("aggregate history len=%d, want 2 (window must exclude bucket %d)", len(rows), bucket-60)
	}
	if rows[0].BucketStart != bucket || rows[0].UploadDelta != 70 || rows[0].DownloadDelta != 700 {
		t.Fatalf("bucket %d aggregate = %+v, want 70/700", bucket, rows[0])
	}
	if rows[1].BucketStart != bucket+60 || rows[1].UploadDelta != 1 || rows[1].DownloadDelta != 2 {
		t.Fatalf("bucket %d aggregate = %+v, want 1/2", bucket+60, rows[1])
	}
	for i, row := range rows {
		if row.ClientID != "" || row.BindingID != "" {
			t.Fatalf("aggregate row %d carries attribution %q/%q, want empty", i, row.ClientID, row.BindingID)
		}
	}

	// The [from,to] window is inclusive on both ends.
	head, err := store.HistoryForAll(0, bucket-60, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(head) != 1 || head[0].BucketStart != bucket-60 || head[0].UploadDelta != 5 || head[0].DownloadDelta != 6 {
		t.Fatalf("window before %d = %+v, want the single bucket-60 row", bucket-60, head)
	}
}

// TestHistoryForAllLimitKeepsNewestBuckets mirrors the per-client history
// contract pinned by TestHistoryLimitReturnsNewestBuckets: LIMIT truncates
// to the NEWEST buckets and the result is returned oldest-first.
func TestHistoryForAllLimitKeepsNewestBuckets(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	c, err := repo.Create(Client{Name: "agg-limit", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	const buckets int64 = 600
	for i := int64(0); i < buckets; i++ {
		if err := store.RecordSample(Sample{BindingID: b.ID, UploadBytes: i + 1, AtUnix: i * 60}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.HistoryForAll(0, (buckets-1)*60, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 500 {
		t.Fatalf("aggregate history len=%d, want 500", len(rows))
	}
	if rows[0].BucketStart != (buckets-500)*60 || rows[len(rows)-1].BucketStart != (buckets-1)*60 {
		t.Fatalf("aggregate window [%d,%d], want newest 500 ending at %d",
			rows[0].BucketStart, rows[len(rows)-1].BucketStart, (buckets-1)*60)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].BucketStart <= rows[i-1].BucketStart {
			t.Fatalf("rows not ascending at %d: %+v after %+v", i, rows[i], rows[i-1])
		}
	}
}

// TestHistoryForAllEmptyStoreReturnsNoRows: an empty store is an empty
// history, not an error — the panel must render "no data", not a failure.
func TestHistoryForAllEmptyStoreReturnsNoRows(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewTrafficStore(db)
	rows, err := store.HistoryForAll(0, time.Now().Unix(), 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty store returned %d rows", len(rows))
	}
}
