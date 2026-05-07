package api

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

const maxApplyHistoryEntries = 100

type ApplyHistoryStore struct {
	path string
	max  int
}

func NewApplyHistoryStore(path string, max int) ApplyHistoryStore {
	if max <= 0 {
		max = maxApplyHistoryEntries
	}
	return ApplyHistoryStore{path: path, max: max}
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
	now := time.Now().UTC()
	entry := ApplyHistoryEntry{
		ID:              now.Format("20060102T150405.000000000Z"),
		Timestamp:       now.Format(time.RFC3339Nano),
		Stage:           stage,
		Success:         success,
		Applied:         response.Applied,
		LiveApplied:     response.LiveApplied,
		ServicesApplied: response.ServicesApplied,
		RolledBack:      response.RolledBack,
		Plan:            response.Plan,
		WrittenFiles:    append([]string(nil), response.WrittenFiles...),
		LiveFiles:       append([]string(nil), response.LiveFiles...),
		BackupFiles:     append([]string(nil), response.BackupFiles...),
		RollbackFiles:   append([]string(nil), response.RollbackFiles...),
		Validations:     append([]ConfigValidationResult(nil), response.Validations...),
		ServiceActions:  append([]ServiceActionResult(nil), response.ServiceActions...),
		HealthChecks:    append([]ServiceHealthResult(nil), response.HealthChecks...),
		RollbackActions: append([]ServiceActionResult(nil), response.RollbackActions...),
	}
	history = append([]ApplyHistoryEntry{entry}, history...)
	if len(history) > s.max {
		history = history[:s.max]
	}
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

func applyHistoryStage(response ApplyResponse) string {
	switch {
	case response.RolledBack:
		return "rollback"
	case response.ServicesApplied:
		return "services"
	case response.LiveApplied:
		return "live"
	default:
		return "staged"
	}
}
