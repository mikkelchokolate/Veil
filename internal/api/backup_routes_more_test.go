package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleBackupRestoreJob(t *testing.T) {
	state := newPanelBackupState(t)
	job := BackupRestoreJob{
		ID:        "job-1",
		Archive:   "veil_backup_test.tar.gz.enc",
		Status:    "succeeded",
		CreatedAt: time.Now().UTC(),
	}
	ownerSession, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "admin"})
	job.ownerSessionToken = ownerSession.Token
	state.backupJobsMu.Lock()
	state.backupJobs[job.ID] = job
	state.backupJobsMu.Unlock()

	get := adminJSONRequest(http.MethodGet, "/api/backup-restore-jobs/job-1", "")
	rec := httptest.NewRecorder()
	state.handleBackupRestoreJob(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(ownerSession.Token); ok {
		t.Fatal("expected owner session to be revoked after reading succeeded job")
	}
}

func TestHandleBackupRestoreJobValidation(t *testing.T) {
	state := newPanelBackupState(t)

	post := adminJSONRequest(http.MethodPost, "/api/backup-restore-jobs/job-1", "")
	rec := httptest.NewRecorder()
	state.handleBackupRestoreJob(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", rec.Code)
	}

	badID := adminJSONRequest(http.MethodGet, "/api/backup-restore-jobs/job/1", "")
	rec = httptest.NewRecorder()
	state.handleBackupRestoreJob(rec, badID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status=%d body=%s", rec.Code, rec.Body.String())
	}

	missing := adminJSONRequest(http.MethodGet, "/api/backup-restore-jobs/missing", "")
	rec = httptest.NewRecorder()
	state.handleBackupRestoreJob(rec, missing)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", rec.Code)
	}
}

func TestHandleBackupPruneValidation(t *testing.T) {
	state := newPanelBackupState(t)

	get := adminJSONRequest(http.MethodGet, "/api/backups/prune", "")
	rec := httptest.NewRecorder()
	state.handleBackupPrune(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}

	viewer := httptest.NewRequest(http.MethodPost, "/api/backups/prune", strings.NewReader(`{"daily":1,"weekly":0,"monthly":0}`))
	viewer.Header.Set("Content-Type", "application/json")
	viewer = viewer.WithContext(context.WithValue(viewer.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleBackupPrune(rec, viewer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", rec.Code)
	}
}

func TestHandleBackupsValidation(t *testing.T) {
	state := newPanelBackupState(t)

	viewer := httptest.NewRequest(http.MethodGet, "/api/backups", nil)
	viewer = viewer.WithContext(context.WithValue(viewer.Context(), contextKeyRole, "viewer"))
	rec := httptest.NewRecorder()
	state.handleBackups(rec, viewer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", rec.Code)
	}

	deleteReq := adminJSONRequest(http.MethodDelete, "/api/backups", "")
	rec = httptest.NewRecorder()
	state.handleBackups(rec, deleteReq)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE status=%d", rec.Code)
	}
}

func TestHandleBackupByNameRestoreConfirmRequired(t *testing.T) {
	state := newPanelBackupState(t)
	restore := adminJSONRequest(http.MethodPost, "/api/backups/veil_backup_test.tar.gz.enc/restore", `{"confirm":false}`)
	rec := httptest.NewRecorder()
	state.handleBackupByName(rec, restore)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQueuePanelBackupRestoreFailsWhenRandomReaderFails(t *testing.T) {
	state := newPanelBackupState(t)
	old := randomReader
	randomReader = func(b []byte) (int, error) {
		return 0, errors.New("random failure")
	}
	t.Cleanup(func() { randomReader = old })

	restore := adminJSONRequest(http.MethodPost, "/api/backups/veil_backup_test.tar.gz.enc/restore", `{"confirm":true}`)
	rec := httptest.NewRecorder()
	state.queuePanelBackupRestore(rec, restore, "veil_backup_test.tar.gz.enc")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
