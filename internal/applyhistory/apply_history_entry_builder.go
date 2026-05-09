package applyhistory

import "time"

type ApplyHistoryEntryBuilder struct {
	now func() time.Time
}

func NewApplyHistoryEntryBuilder(now func() time.Time) ApplyHistoryEntryBuilder {
	if now == nil {
		now = time.Now
	}
	return ApplyHistoryEntryBuilder{now: now}
}

func (b ApplyHistoryEntryBuilder) Build(stage string, success bool, response ApplyResponse) ApplyHistoryEntry {
	now := b.now().UTC()
	return ApplyHistoryEntry{
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
}
