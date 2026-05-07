package api

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

func TestApplyHistoryFilterRejectsInvalidFilterDeterministically(t *testing.T) {
	_, err := NewApplyHistoryFilter(map[string][]string{"z": {"1"}, "bad": {"1"}}).Apply(nil)
	if err == nil || err.Error() != "invalid history filter: bad" {
		t.Fatalf("err = %v", err)
	}
}
