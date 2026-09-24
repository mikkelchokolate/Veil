package applyhistory

import (
	"path/filepath"
	"testing"
)

func TestApplyHistoryStoreAppendPrependsAndCapsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	const configuredMax = 2
	store := NewApplyHistoryStore(path, configuredMax)

	for _, stage := range []string{"staged", "live", "services"} {
		if err := store.Append(stage, true, ApplyResponse{Applied: true}); err != nil {
			t.Fatalf("Append(%s): %v", stage, err)
		}
	}
	history, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The persisted file honors exactly the configured retention maximum:
	// newest-first ordering, oldest entry evicted.
	if len(history) != NewApplyHistoryRetention(configuredMax).Max() {
		t.Fatalf("history length = %d, want retention max %d", len(history), NewApplyHistoryRetention(configuredMax).Max())
	}
	if history[0].Stage != "services" || history[1].Stage != "live" {
		t.Fatalf("history ordering/cap = %+v", history)
	}
	for _, entry := range history {
		if entry.Stage == "staged" {
			t.Fatalf("evicted entry persisted past retention max: %+v", history)
		}
	}
}

// #968: an ambiguous apply outcome must round-trip durably — appended under
// the "ambiguous" stage, reloaded from disk with Ambiguous preserved, and
// selectable through the stage=ambiguous history filter.
func TestApplyHistoryStoreRoundTripsAmbiguousEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	history := NewApplyHistory(path, 10)

	ambiguous := ApplyResponse{MutationStarted: true, Ambiguous: true}
	if err := history.Append(HistoryStage(ambiguous), false, ambiguous); err != nil {
		t.Fatalf("Append(ambiguous): %v", err)
	}
	if err := history.Append("staged", true, ApplyResponse{LiveApplied: true}); err != nil {
		t.Fatalf("Append(staged): %v", err)
	}

	entries, err := history.Query(map[string][]string{"stage": {"ambiguous"}})
	if err != nil {
		t.Fatalf("Query(stage=ambiguous): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("stage=ambiguous must match exactly the ambiguous entry, got %+v", entries)
	}
	if !entries[0].Ambiguous || entries[0].Stage != "ambiguous" || entries[0].Success {
		t.Fatalf("persisted ambiguous entry lost its evidence: %+v", entries[0])
	}
}
