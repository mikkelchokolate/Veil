package api

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBeginBackupMutationPrunesOldTerminalJobs(t *testing.T) {
	state := &managementState{backupJobs: make(map[string]BackupRestoreJob)}
	base := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	state.backupJobs["running"] = BackupRestoreJob{
		ID:        "running",
		Status:    "running",
		CreatedAt: base.Add(-time.Hour),
	}
	for i := 0; i < maxRetainedBackupRestoreJobs-1; i++ {
		id := fmt.Sprintf("job-%03d", i)
		state.backupJobs[id] = BackupRestoreJob{
			ID:         id,
			Status:     "succeeded",
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
			FinishedAt: base.Add(time.Duration(i) * time.Minute),
		}
	}

	response := httptest.NewRecorder()
	if !state.beginBackupMutation(response) {
		t.Fatalf("failed to acquire backup mutation guard: status=%d body=%s", response.Code, response.Body.String())
	}
	defer state.backupMutationMu.Unlock()

	if got := len(state.backupJobs); got != maxRetainedBackupRestoreJobs-1 {
		t.Fatalf("retained job count = %d, want %d", got, maxRetainedBackupRestoreJobs-1)
	}
	if _, ok := state.backupJobs["running"]; !ok {
		t.Fatal("active restore job was pruned")
	}
	if _, ok := state.backupJobs["job-000"]; ok {
		t.Fatal("oldest terminal restore job was not pruned")
	}
	if _, ok := state.backupJobs[fmt.Sprintf("job-%03d", maxRetainedBackupRestoreJobs-2)]; !ok {
		t.Fatal("newest terminal restore job was pruned")
	}
}

func TestBackupRestorePruningLeavesSmallRegistryUntouched(t *testing.T) {
	state := &managementState{backupJobs: map[string]BackupRestoreJob{
		"failed": {ID: "failed", Status: "failed", CreatedAt: time.Now().UTC()},
	}}
	state.pruneBackupRestoreJobs()
	if _, ok := state.backupJobs["failed"]; !ok {
		t.Fatal("small restore registry was pruned")
	}
}
