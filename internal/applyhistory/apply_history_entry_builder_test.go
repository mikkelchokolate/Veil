package applyhistory

import (
	"reflect"
	"testing"
	"time"
)

func TestApplyHistoryEntryBuilderCopiesApplyResponseFields(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 30, 45, 123, time.UTC)
	response := ApplyResponse{
		Applied:         true,
		LiveApplied:     true,
		ServicesApplied: true,
		RolledBack:      true,
		Ambiguous:       true,
		Plan:            ApplyPlanResponse{Valid: true},
		WrittenFiles:    []string{"a"},
		LiveFiles:       []string{"b"},
		BackupFiles:     []string{"c"},
		RollbackFiles:   []string{"d"},
		Validations:     []ConfigValidationResult{{Name: "v"}},
		ServiceActions:  []ServiceActionResult{{Name: "sa"}},
		HealthChecks:    []ServiceHealthResult{{Name: "h"}},
		RollbackActions: []ServiceActionResult{{Name: "ra"}},
	}
	entry := NewApplyHistoryEntryBuilder(func() time.Time { return now }).Build("live", true, response)
	if entry.ID != "20260507T123045.000000123Z" || entry.Timestamp != now.Format(time.RFC3339Nano) {
		t.Fatalf("entry time fields = %+v", entry)
	}
	if entry.Stage != "live" || !entry.Success {
		t.Fatalf("entry stage/success = %+v", entry)
	}
	// Every evidence field must be copied — mutation and ambiguity evidence is
	// part of the entry, not dropped on the way into history (#912).
	if !entry.Applied || !entry.LiveApplied || !entry.ServicesApplied || !entry.RolledBack || !entry.Ambiguous {
		t.Fatalf("entry dropped mutation/ambiguity evidence: %+v", entry)
	}
	if !entry.Plan.Valid {
		t.Fatalf("entry dropped plan: %+v", entry)
	}
	if !reflect.DeepEqual(entry.WrittenFiles, response.WrittenFiles) ||
		!reflect.DeepEqual(entry.LiveFiles, response.LiveFiles) ||
		!reflect.DeepEqual(entry.BackupFiles, response.BackupFiles) ||
		!reflect.DeepEqual(entry.RollbackFiles, response.RollbackFiles) ||
		!reflect.DeepEqual(entry.Validations, response.Validations) ||
		!reflect.DeepEqual(entry.ServiceActions, response.ServiceActions) ||
		!reflect.DeepEqual(entry.HealthChecks, response.HealthChecks) ||
		!reflect.DeepEqual(entry.RollbackActions, response.RollbackActions) {
		t.Fatalf("entry dropped evidence slices: %+v", entry)
	}
	// The slices must be copies, not aliases of the response's backing arrays.
	response.WrittenFiles[0] = "mutated"
	response.LiveFiles[0] = "mutated"
	response.BackupFiles[0] = "mutated"
	response.RollbackFiles[0] = "mutated"
	response.Validations[0].Name = "mutated"
	response.ServiceActions[0].Name = "mutated"
	response.HealthChecks[0].Name = "mutated"
	response.RollbackActions[0].Name = "mutated"
	if entry.WrittenFiles[0] != "a" || entry.LiveFiles[0] != "b" || entry.BackupFiles[0] != "c" ||
		entry.RollbackFiles[0] != "d" || entry.Validations[0].Name != "v" || entry.ServiceActions[0].Name != "sa" ||
		entry.HealthChecks[0].Name != "h" || entry.RollbackActions[0].Name != "ra" {
		t.Fatalf("entry aliases the response's slices: %+v", entry)
	}
}
