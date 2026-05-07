package api

import (
	"testing"
	"time"
)

func TestApplyHistoryEntryBuilderCopiesApplyResponseFields(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 30, 45, 123, time.UTC)
	response := ApplyResponse{Applied: true, LiveApplied: true, WrittenFiles: []string{"a"}, Plan: ApplyPlanResponse{Valid: true}}
	entry := NewApplyHistoryEntryBuilder(func() time.Time { return now }).Build("live", true, response)
	if entry.ID != "20260507T123045.000000123Z" || entry.Timestamp != now.Format(time.RFC3339Nano) {
		t.Fatalf("entry time fields = %+v", entry)
	}
	if entry.Stage != "live" || !entry.Success || !entry.Applied || !entry.LiveApplied || !entry.Plan.Valid {
		t.Fatalf("entry = %+v", entry)
	}
	response.WrittenFiles[0] = "mutated"
	if entry.WrittenFiles[0] != "a" {
		t.Fatalf("entry did not copy written files: %+v", entry.WrittenFiles)
	}
}
