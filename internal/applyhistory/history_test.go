package applyhistory

import (
	"path/filepath"
	"testing"
)

func TestHistoryStageReturnsCorrectStage(t *testing.T) {
	tests := []struct {
		name     string
		response ApplyResponse
		want     string
	}{
		{name: "rollback stage", response: ApplyResponse{RolledBack: true}, want: "rollback"},
		{name: "services stage supersedes live", response: ApplyResponse{ServicesApplied: true, LiveApplied: true}, want: "services"},
		{name: "live stage", response: ApplyResponse{LiveApplied: true}, want: "live"},
		{name: "staged fallback", response: ApplyResponse{}, want: "staged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HistoryStage(tt.response)
			if got != tt.want {
				t.Fatalf("HistoryStage() = %q, want %q", got, tt.want)
			}
		})
	}
}

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
