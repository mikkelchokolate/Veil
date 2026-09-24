package applyhistory

import "testing"

func TestApplyHistoryFilterFiltersByStageSuccessAndLimit(t *testing.T) {
	history := []ApplyHistoryEntry{
		{ID: "1", Stage: "services", Success: true},
		{ID: "2", Stage: "live", Success: true},
		{ID: "3", Stage: "services", Success: false},
	}
	filtered, err := NewApplyHistoryFilter(map[string][]string{"stage": {"services"}, "success": {"true"}, "limit": {"1"}}).Apply(history)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "1" {
		t.Fatalf("filtered = %+v", filtered)
	}
}

// #968: "ambiguous" is a first-class history stage — the filter must accept it
// and return exactly the entries whose runtime outcome could not be proven.
func TestApplyHistoryFilterAcceptsAmbiguousStage(t *testing.T) {
	history := []ApplyHistoryEntry{
		{ID: "1", Stage: "staged", Success: false},
		{ID: "2", Stage: "ambiguous", Success: false, Ambiguous: true},
		{ID: "3", Stage: "rollback", Success: false},
	}
	filtered, err := NewApplyHistoryFilter(map[string][]string{"stage": {"ambiguous"}}).Apply(history)
	if err != nil {
		t.Fatalf("Apply(stage=ambiguous): %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "2" || !filtered[0].Ambiguous {
		t.Fatalf("filtered = %+v, want only the ambiguous entry", filtered)
	}
}

func TestApplyHistoryFilterRejectsInvalidFilterDeterministically(t *testing.T) {
	_, err := NewApplyHistoryFilter(map[string][]string{"z": {"1"}, "bad": {"1"}}).Apply(nil)
	if err == nil || err.Error() != "invalid history filter: bad" {
		t.Fatalf("err = %v", err)
	}
}
