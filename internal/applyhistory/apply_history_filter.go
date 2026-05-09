package applyhistory

import (
	"fmt"
	"sort"
	"strconv"
)

var allowedApplyHistoryStages = map[string]bool{
	"staged":     true,
	"live":       true,
	"services":   true,
	"rollback":   true,
	"validation": true,
}

var allowedApplyHistoryFilters = map[string]bool{
	"stage":   true,
	"success": true,
	"limit":   true,
}

type ApplyHistoryFilter struct {
	values map[string][]string
}

func NewApplyHistoryFilter(values map[string][]string) ApplyHistoryFilter {
	return ApplyHistoryFilter{values: values}
}

func (f ApplyHistoryFilter) Apply(history []ApplyHistoryEntry) ([]ApplyHistoryEntry, error) {
	if err := f.validateKeys(); err != nil {
		return nil, err
	}
	stage := firstQueryValue(f.values, "stage")
	successText := firstQueryValue(f.values, "success")
	limitText := firstQueryValue(f.values, "limit")
	var successFilter *bool
	if stage != "" && !allowedApplyHistoryStages[stage] {
		return nil, fmt.Errorf("invalid stage filter: %s", stage)
	}
	if successText != "" {
		parsed, err := strconv.ParseBool(successText)
		if err != nil {
			return nil, fmt.Errorf("invalid success filter: %s", successText)
		}
		successFilter = &parsed
	}
	limit := 0
	if limitText != "" {
		parsed, err := strconv.Atoi(limitText)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid limit: %s", limitText)
		}
		limit = parsed
	}
	filtered := make([]ApplyHistoryEntry, 0, len(history))
	for _, entry := range history {
		if stage != "" && entry.Stage != stage {
			continue
		}
		if successFilter != nil && entry.Success != *successFilter {
			continue
		}
		filtered = append(filtered, entry)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

func (f ApplyHistoryFilter) validateKeys() error {
	filterKeys := make([]string, 0, len(f.values))
	for key := range f.values {
		filterKeys = append(filterKeys, key)
	}
	sort.Strings(filterKeys)
	for _, key := range filterKeys {
		if !allowedApplyHistoryFilters[key] {
			return fmt.Errorf("invalid history filter: %s", key)
		}
	}
	return nil
}

func filterApplyHistory(history []ApplyHistoryEntry, values map[string][]string) ([]ApplyHistoryEntry, error) {
	return NewApplyHistoryFilter(values).Apply(history)
}
