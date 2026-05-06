package api

import (
	"path/filepath"
	"testing"
)

func TestApplyHistoryStoreAppendPrependsAndCapsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	store := NewApplyHistoryStore(path, 2)

	for _, stage := range []string{"staged", "live", "services"} {
		if err := store.Append(stage, true, ApplyResponse{Applied: true}); err != nil {
			t.Fatalf("Append(%s): %v", stage, err)
		}
	}
	history, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].Stage != "services" || history[1].Stage != "live" {
		t.Fatalf("history ordering/cap = %+v", history)
	}
}
