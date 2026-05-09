package api

import (
	"path/filepath"
	"testing"
)

func TestApplyHistoryAppendsRetainsAndQueriesThroughOneModule(t *testing.T) {
	history := NewApplyHistory(filepath.Join(t.TempDir(), "apply-history.json"), 2)
	if err := history.Append("staged", true, ApplyResponse{Applied: true}); err != nil {
		t.Fatalf("Append staged: %v", err)
	}
	if err := history.Append("services", false, ApplyResponse{Applied: true, ServicesApplied: true}); err != nil {
		t.Fatalf("Append services: %v", err)
	}
	if err := history.Append("rollback", false, ApplyResponse{Applied: true, RolledBack: true}); err != nil {
		t.Fatalf("Append rollback: %v", err)
	}

	filtered, err := history.Query(map[string][]string{"success": {"false"}, "limit": {"1"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Stage != "rollback" || !filtered[0].RolledBack {
		t.Fatalf("filtered = %+v", filtered)
	}

	all, err := history.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(all) != 2 || all[0].Stage != "rollback" || all[1].Stage != "services" {
		t.Fatalf("retained history = %+v", all)
	}
}
