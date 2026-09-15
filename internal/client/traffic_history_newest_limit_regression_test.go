package client

import "testing"

func TestHistoryLimitReturnsNewestBuckets(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	c, err := repo.Create(Client{Name: "hist", Enabled: true, QuotaResetPolicy: ResetNever})
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
	newest := (buckets - 1) * 60
	oldestKept := (buckets - 500) * 60
	clientRows, err := store.HistoryForClient(c.ID, 0, newest, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(clientRows) != 500 {
		t.Fatalf("client history len=%d, want 500", len(clientRows))
	}
	if clientRows[0].BucketStart != oldestKept || clientRows[len(clientRows)-1].BucketStart != newest {
		t.Fatalf("client history window [%d,%d], want newest 500 ending at %d", clientRows[0].BucketStart, clientRows[len(clientRows)-1].BucketStart, newest)
	}
	bindingRows, err := store.HistoryForBinding(b.ID, 0, newest, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindingRows) != 500 || bindingRows[0].BucketStart != oldestKept || bindingRows[len(bindingRows)-1].BucketStart != newest {
		t.Fatalf("binding history window [%d,%d] len=%d", bindingRows[0].BucketStart, bindingRows[len(bindingRows)-1].BucketStart, len(bindingRows))
	}
}
