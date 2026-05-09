package applyhistory

import "testing"

func TestApplyHistoryRetentionPrependsAndKeepsNewestEntries(t *testing.T) {
	retention := NewApplyHistoryRetention(2)
	kept := retention.Prepend(ApplyHistoryEntry{ID: "new"}, []ApplyHistoryEntry{{ID: "old1"}, {ID: "old2"}})
	if len(kept) != 2 || kept[0].ID != "new" || kept[1].ID != "old1" {
		t.Fatalf("kept = %+v", kept)
	}
}

func TestApplyHistoryRetentionDefaultsInvalidMax(t *testing.T) {
	kept := NewApplyHistoryRetention(0).Prepend(ApplyHistoryEntry{ID: "new"}, nil)
	if len(kept) != 1 || kept[0].ID != "new" {
		t.Fatalf("kept = %+v", kept)
	}
}
