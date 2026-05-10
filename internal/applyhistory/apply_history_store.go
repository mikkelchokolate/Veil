package applyhistory

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/veil-panel/veil/internal/applyflow"
)

const MaxEntries = 100

type ApplyHistoryStore struct {
	path string
	max  int
}

func NewApplyHistoryStore(path string, max int) ApplyHistoryStore {
	return ApplyHistoryStore{path: path, max: NewApplyHistoryRetention(max).Max()}
}

func (s ApplyHistoryStore) Load() ([]ApplyHistoryEntry, error) {
	body, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ApplyHistoryEntry{}, nil
		}
		return nil, err
	}
	var history []ApplyHistoryEntry
	if err := json.Unmarshal(body, &history); err != nil {
		return nil, err
	}
	return history, nil
}

func (s ApplyHistoryStore) Append(stage string, success bool, response ApplyResponse) error {
	history, err := s.Load()
	if err != nil {
		return err
	}
	entry := NewApplyHistoryEntryBuilder(nil).Build(stage, success, response)
	history = NewApplyHistoryRetention(s.max).Prepend(entry, history)
	body, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(s.path, append(body, '\n'), 0o600)
}

func firstQueryValue(values map[string][]string, key string) string {
	if values == nil || len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func HistoryStage(response ApplyResponse) string {
	return applyflow.HistoryStage(response)
}
