package applyhistory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyHistoryQueryReturnsLoadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	history := NewApplyHistory(path, 10)
	_, err := history.Query(map[string][]string{})
	if err == nil {
		t.Fatal("expected Query to return a load error")
	}
}

func TestApplyHistoryFilterValidationAndMatching(t *testing.T) {
	history := []ApplyHistoryEntry{
		{ID: "1", Stage: "staged", Success: true},
		{ID: "2", Stage: "live", Success: true},
		{ID: "3", Stage: "services", Success: false},
		{ID: "4", Stage: "rollback", Success: false},
	}

	tests := []struct {
		name    string
		values  map[string][]string
		wantErr string
		wantIDs []string
	}{
		{
			name:    "invalid stage",
			values:  map[string][]string{"stage": {"unknown"}},
			wantErr: "invalid stage filter: unknown",
		},
		{
			name:    "invalid success",
			values:  map[string][]string{"success": {"maybe"}},
			wantErr: "invalid success filter: maybe",
		},
		{
			name:    "invalid limit text",
			values:  map[string][]string{"limit": {"abc"}},
			wantErr: "invalid limit: abc",
		},
		{
			name:    "negative limit",
			values:  map[string][]string{"limit": {"-1"}},
			wantErr: "invalid limit: -1",
		},
		{
			name:    "stage filter skips non-matching",
			values:  map[string][]string{"stage": {"live"}},
			wantIDs: []string{"2"},
		},
		{
			name:    "success filter skips non-matching",
			values:  map[string][]string{"success": {"false"}},
			wantIDs: []string{"3", "4"},
		},
		{
			name:    "combined filters with limit",
			values:  map[string][]string{"stage": {"services"}, "success": {"false"}, "limit": {"1"}},
			wantIDs: []string{"3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered, err := NewApplyHistoryFilter(tt.values).Apply(history)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(filtered) != len(tt.wantIDs) {
				t.Fatalf("filtered length = %d, want %d", len(filtered), len(tt.wantIDs))
			}
			for i, id := range tt.wantIDs {
				if filtered[i].ID != id {
					t.Fatalf("filtered[%d].ID = %q, want %q", i, filtered[i].ID, id)
				}
			}
		})
	}
}

func TestApplyHistoryStoreLoadReturnsReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	store := NewApplyHistoryStore(path, 10)
	_, err := store.Load()
	if err == nil {
		t.Fatal("expected Load to return a read error")
	}
}

func TestApplyHistoryStoreLoadReturnsUnmarshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := NewApplyHistoryStore(path, 10)
	_, err := store.Load()
	if err == nil {
		t.Fatal("expected Load to return an unmarshal error")
	}
}

func TestApplyHistoryStoreAppendReturnsLoadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	store := NewApplyHistoryStore(path, 10)
	err := store.Append("staged", true, ApplyResponse{})
	if err == nil {
		t.Fatal("expected Append to return a load error")
	}
}

func TestApplyHistoryStoreAppendReturnsMarshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apply-history.json")
	store := NewApplyHistoryStore(path, 10)

	original := jsonMarshalIndent
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	defer func() { jsonMarshalIndent = original }()

	err := store.Append("staged", true, ApplyResponse{})
	if err == nil || err.Error() != "marshal failed" {
		t.Fatalf("err = %v, want marshal failed", err)
	}
}
