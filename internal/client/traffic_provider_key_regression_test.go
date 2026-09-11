package client

import (
	"strings"
	"testing"
	"time"
)

func TestRecordSampleAcceptsLongHysteria2CompositeProviderKey(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	row, err := repo.Create(Client{Name: "long-key", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: strings.Repeat("n", 114), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	key := "hysteria2:" + strings.Repeat("n", 114) + ":" + binding.ID
	if len(key) <= 160 {
		t.Fatalf("fixture key too short: %d", len(key))
	}
	if err := store.RecordSample(Sample{
		BindingID: binding.ID, UploadBytes: 10, DownloadBytes: 20, AtUnix: time.Now().Unix(),
		Monotonic: true, ProviderKey: key,
	}); err != nil {
		t.Fatalf("long composite provider key rejected: %v", err)
	}
}
