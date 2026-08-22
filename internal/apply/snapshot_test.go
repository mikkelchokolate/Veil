package apply

import (
	"testing"
)

func TestSnapshotStoreRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ss := NewSnapshotStore(db)

	payload := []byte(`{"inbounds":[{"name":"a"}],"settings":{}}`)
	if err := ss.Save(41, payload); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := ss.Load(41)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("round trip mismatch: got %s", got)
	}
}

func TestSnapshotStoreImmutable(t *testing.T) {
	db := openTestDB(t)
	ss := NewSnapshotStore(db)

	if err := ss.Save(42, []byte(`{"v":42}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Saving the same revision again must NOT overwrite the original snapshot:
	// a revision is immutable once recorded.
	if err := ss.Save(42, []byte(`{"v":999}`)); err != nil {
		t.Fatalf("re-save (idempotent) should not error: %v", err)
	}
	got, _ := ss.Load(42)
	if string(got) != `{"v":42}` {
		t.Fatalf("snapshot was overwritten: %s", got)
	}
}

func TestSnapshotStoreLoadMissing(t *testing.T) {
	db := openTestDB(t)
	ss := NewSnapshotStore(db)
	if _, err := ss.Load(999); err == nil {
		t.Fatal("expected error loading missing snapshot")
	}
}
