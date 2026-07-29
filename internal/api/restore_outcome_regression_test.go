package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type restoreOutcomePrivilegedClient struct {
	*recordingPrivilegedClient
	result      privileged.BackupResult
	restoreErr  error
	beforeReply func()
}

func (c *restoreOutcomePrivilegedClient) Backup(ctx context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	if request.Action == privileged.BackupActionRestore {
		if c.beforeReply != nil {
			c.beforeReply()
		}
		return c.result, c.restoreErr
	}
	return c.recordingPrivilegedClient.Backup(ctx, request)
}

func TestRestoreJobsPersistTruthfulOutcomeAndPhase(t *testing.T) {
	tests := []struct {
		name           string
		helperRestored bool
		helperPhase    string
		helperOutcome  string
		helperErr      error
		breakReload    bool
		wantStatus     string
		wantOutcome    string
		wantPhase      string
		wantRestored   bool
		wantHTTP       int
	}{
		{name: "not_restored", helperPhase: "restore_failed", helperOutcome: "not_restored", helperErr: errors.New("restore rejected"), wantStatus: "failed", wantOutcome: "not_restored", wantPhase: "restore_failed", wantHTTP: http.StatusInternalServerError},
		{name: "pending_key_publication", helperPhase: "key_publication_pending", helperOutcome: "pending_key_publication", helperErr: errors.New("key publication pending"), wantStatus: "pending", wantOutcome: "pending_key_publication", wantPhase: "key_publication_pending", wantHTTP: http.StatusAccepted},
		{name: "finalization_failure_after_commit", helperRestored: true, helperPhase: "finalization_failed", helperOutcome: "restored", helperErr: errors.New("restore journal finalization failed"), wantStatus: "degraded", wantOutcome: "restored", wantPhase: "finalization_failed", wantRestored: true, wantHTTP: http.StatusInternalServerError},
		{name: "revalidation_failure_after_commit", helperRestored: true, helperPhase: "committed", helperOutcome: "restored", breakReload: true, wantStatus: "degraded", wantOutcome: "restored", wantPhase: "revalidation_failed", wantRestored: true, wantHTTP: http.StatusInternalServerError},
		{name: "restored", helperRestored: true, helperPhase: "committed", helperOutcome: "restored", wantStatus: "succeeded", wantOutcome: "restored", wantPhase: "completed", wantRestored: true, wantHTTP: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, state := newApplyTrackedRouterWithState(t)
			result := privileged.BackupResult{ArchiveName: "veil_backup_test.tar.gz", Verified: true, Restored: test.helperRestored}
			setFutureStringField(t, &result, "Phase", test.helperPhase)
			setFutureStringField(t, &result, "Outcome", test.helperOutcome)
			client := &restoreOutcomePrivilegedClient{recordingPrivilegedClient: &recordingPrivilegedClient{}, result: result, restoreErr: test.helperErr}
			if test.breakReload {
				client.beforeReply = func() {
					if err := os.WriteFile(state.statePath, []byte("{broken restored state"), 0o600); err != nil {
						t.Errorf("corrupt restored state: %v", err)
					}
				}
			}
			state.privileged = client
			state.privilegedLocal = false
			jobID := "restore-outcome-" + test.name
			state.backupJobsMu.Lock()
			state.backupJobs[jobID] = BackupRestoreJob{ID: jobID, Archive: result.ArchiveName, Status: "queued", CreatedAt: time.Now().UTC()}
			if err := state.persistBackupRestoreJobsLocked(); err != nil {
				state.backupJobsMu.Unlock()
				t.Fatal(err)
			}
			state.backupJobsMu.Unlock()
			state.backupMutationMu.Lock()
			state.runPanelBackupRestore(jobID, result.ArchiveName, "", "admin", "admin", "127.0.0.1", "test")

			job, ok := state.backupRestoreJob(jobID)
			if !ok {
				t.Fatal("restore job disappeared")
			}
			assertRestoreJobOutcomeJSON(t, job, test.wantStatus, test.wantOutcome, test.wantPhase, test.wantRestored, test.wantHTTP)

			reloaded := &managementState{backupJobsPath: state.backupJobsPath, backupJobs: make(map[string]BackupRestoreJob)}
			if err := reloaded.loadBackupRestoreJobs(); err != nil {
				t.Fatal(err)
			}
			persisted, ok := reloaded.backupJobs[jobID]
			if !ok {
				t.Fatal("persisted restore job disappeared")
			}
			assertRestoreJobOutcomeJSON(t, persisted, test.wantStatus, test.wantOutcome, test.wantPhase, test.wantRestored, test.wantHTTP)
		})
	}
}

func setFutureStringField(t *testing.T, target any, name, value string) {
	t.Helper()
	field := reflect.ValueOf(target).Elem().FieldByName(name)
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		t.Errorf("%T is missing required %s field", target, name)
		return
	}
	field.SetString(value)
}

func assertRestoreJobOutcomeJSON(t *testing.T, job BackupRestoreJob, status, outcome, phase string, restored bool, httpStatus int) {
	t.Helper()
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	if got, _ := document["status"].(string); got != status {
		t.Errorf("status=%q want=%q document=%s", got, status, body)
	}
	if got, _ := document["outcome"].(string); got != outcome {
		t.Errorf("outcome=%q want=%q document=%s", got, outcome, body)
	}
	if got, _ := document["phase"].(string); got != phase {
		t.Errorf("phase=%q want=%q document=%s", got, phase, body)
	}
	if got, ok := document["restored"].(bool); !ok || got != restored {
		t.Errorf("restored=%v present=%v want=%v document=%s", got, ok, restored, body)
	}
	if got, ok := document["httpStatus"].(float64); !ok || int(got) != httpStatus {
		t.Errorf("httpStatus=%v present=%v want=%d document=%s", got, ok, httpStatus, body)
	}
}
