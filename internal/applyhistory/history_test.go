package applyhistory

import (
	"path/filepath"
	"testing"
)

func TestHistoryStoreAppendsAndFiltersEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	history := NewHistory(path, 10)
	if err := history.Append("staged", true, ApplyResponse{Applied: true}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries, err := history.Query(map[string][]string{"stage": {"staged"}, "success": {"true"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 || entries[0].Stage != "staged" || !entries[0].Applied {
		t.Fatalf("entries = %+v", entries)
	}
}
