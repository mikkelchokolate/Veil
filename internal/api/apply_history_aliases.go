package api

import (
	"time"

	"github.com/veil-panel/veil/internal/applyhistory"
)

const maxApplyHistoryEntries = applyhistory.MaxEntries

type ApplyHistory = applyhistory.ApplyHistory
type ApplyHistoryStore = applyhistory.ApplyHistoryStore
type ApplyHistoryEntryBuilder = applyhistory.ApplyHistoryEntryBuilder
type ApplyHistoryFilter = applyhistory.ApplyHistoryFilter
type ApplyHistoryRetention = applyhistory.ApplyHistoryRetention

func NewApplyHistory(path string, max int) ApplyHistory {
	return applyhistory.NewApplyHistory(path, max)
}
func NewApplyHistoryStore(path string, max int) ApplyHistoryStore {
	return applyhistory.NewApplyHistoryStore(path, max)
}
func NewApplyHistoryEntryBuilder(now func() time.Time) ApplyHistoryEntryBuilder {
	return applyhistory.NewApplyHistoryEntryBuilder(now)
}
func NewApplyHistoryFilter(values map[string][]string) ApplyHistoryFilter {
	return applyhistory.NewApplyHistoryFilter(values)
}
func NewApplyHistoryRetention(max int) ApplyHistoryRetention {
	return applyhistory.NewApplyHistoryRetention(max)
}
func applyHistoryStage(response ApplyResponse) string { return applyhistory.HistoryStage(response) }
