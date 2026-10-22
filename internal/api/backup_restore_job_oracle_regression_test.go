package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func viewerRestoreJobRequest(id, sessionToken string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/backup-restore-jobs/"+id, nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "viewer"))
	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: sessionToken})
	}
	return req
}

// #1268 item 2: a non-admin who does not own a job must not be able to tell
// an existing job id from an unknown one.
func TestBackupRestoreJobNonOwnerCannotProbeExistence(t *testing.T) {
	state := newPanelBackupState(t)
	owner, err := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := state.sessionRegistry().Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	state.backupJobsMu.Lock()
	state.backupJobs["job-1"] = BackupRestoreJob{ID: "job-1", Archive: "veil_backup_test.tar.gz.enc", Status: "succeeded",
		HTTPStatus: http.StatusOK, CreatedAt: time.Now().UTC(), ownerSessionToken: owner.Token}
	state.backupJobs["job-unowned"] = BackupRestoreJob{ID: "job-unowned", Archive: "veil_backup_test.tar.gz.enc", Status: "failed",
		HTTPStatus: http.StatusInternalServerError, CreatedAt: time.Now().UTC()}
	state.backupJobsMu.Unlock()

	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		state.handleBackupRestoreJob(rec, req)
		return rec
	}
	missing := serve(viewerRestoreJobRequest("job-missing", other.Token))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown id: status=%d body=%s", missing.Code, missing.Body.String())
	}
	for _, tc := range []struct{ name, id, token string }{
		{"other viewer's session", "job-1", other.Token},
		{"no session cookie", "job-1", ""},
		{"forged cookie", "job-1", "not-a-session"},
		{"job without an owner", "job-unowned", other.Token},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(viewerRestoreJobRequest(tc.id, tc.token))
			if rec.Code != missing.Code || rec.Body.String() != missing.Body.String() {
				t.Fatalf("existing job answered %d %q, unknown id answered %d %q; the two must be indistinguishable",
					rec.Code, rec.Body.String(), missing.Code, missing.Body.String())
			}
		})
	}

	// An empty cookie value must not "match" a job that has no owner token.
	emptyCookie := viewerRestoreJobRequest("job-unowned", "")
	emptyCookie.Header.Set("Cookie", "veil_session=")
	if rec := serve(emptyCookie); rec.Code != missing.Code || rec.Body.String() != missing.Body.String() {
		t.Fatalf("empty cookie on an unowned job answered %d %q, want the unknown-id response", rec.Code, rec.Body.String())
	}

	if rec := serve(viewerRestoreJobRequest("job-1", owner.Token)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"job-1"`) {
		t.Fatalf("owner read: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := serve(adminJSONRequest(http.MethodGet, "/api/backup-restore-jobs/job-unowned", "")); rec.Code != http.StatusInternalServerError ||
		!strings.Contains(rec.Body.String(), `"id":"job-unowned"`) {
		t.Fatalf("admin read: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := serve(adminJSONRequest(http.MethodGet, "/api/backup-restore-jobs/job-missing", "")); rec.Code != http.StatusNotFound {
		t.Fatalf("admin unknown id: status=%d", rec.Code)
	}
}

// #1268 item 4: backup-restore-jobs.json lives in the veil-writable state
// dir. An entry the panel could not have written (unknown status, or an HTTP
// status WriteHeader panics on) must reject the history as a whole.
func TestBackupRestoreJobHistoryRejectsForgedStatus(t *testing.T) {
	valid := BackupRestoreJob{ID: "job-ok", Archive: "veil_backup_test.tar.gz.enc", Status: "succeeded", HTTPStatus: http.StatusOK}
	for _, tc := range []struct {
		name    string
		forged  BackupRestoreJob
		wantErr bool
	}{
		{"legacy unset http status", BackupRestoreJob{Status: "failed"}, false},
		{"pending key publication", BackupRestoreJob{Status: "pending", HTTPStatus: http.StatusAccepted}, false},
		{"locked", BackupRestoreJob{Status: "failed", HTTPStatus: http.StatusLocked}, false},
		{"unknown status", BackupRestoreJob{Status: "pwned", HTTPStatus: http.StatusOK}, true},
		{"empty status", BackupRestoreJob{HTTPStatus: http.StatusOK}, true},
		{"status that panics WriteHeader", BackupRestoreJob{Status: "failed", HTTPStatus: 99}, true},
		{"negative status", BackupRestoreJob{Status: "failed", HTTPStatus: -1}, true},
		{"informational status", BackupRestoreJob{Status: "failed", HTTPStatus: http.StatusSwitchingProtocols}, true},
		{"status above 599", BackupRestoreJob{Status: "failed", HTTPStatus: 600}, true},
		{"status above 999", BackupRestoreJob{Status: "failed", HTTPStatus: 1000}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "backup-restore-jobs.json")
			forged := tc.forged
			forged.ID, forged.Archive = "job-forged", "veil_backup_test.tar.gz.enc"
			body, err := json.Marshal(backupRestoreJobStoreFile{Version: 1, Jobs: []BackupRestoreJob{valid, forged}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			state := &managementState{backupJobsPath: path, backupJobs: make(map[string]BackupRestoreJob)}
			err = state.loadBackupRestoreJobs()
			if (err != nil) != tc.wantErr {
				t.Fatalf("load error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && len(state.backupJobs) != 0 {
				t.Fatalf("rejected history was half-loaded: %v", state.backupJobs)
			}
			if !tc.wantErr && len(state.backupJobs) != 2 {
				t.Fatalf("valid history not loaded: %v", state.backupJobs)
			}
		})
	}
}

// Defense in depth for item 4: even an in-memory job with an unservable
// HTTP status must answer 500, not panic the request.
func TestBackupRestoreJobUnservableHTTPStatusAnswers500(t *testing.T) {
	state := newPanelBackupState(t)
	for _, code := range []int{99, 101, 1000} {
		state.backupJobsMu.Lock()
		state.backupJobs["job-1"] = BackupRestoreJob{ID: "job-1", Archive: "veil_backup_test.tar.gz.enc", Status: "failed", HTTPStatus: code}
		state.backupJobsMu.Unlock()
		rec := httptest.NewRecorder()
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("httpStatus %d panicked: %v", code, p)
				}
			}()
			state.handleBackupRestoreJob(rec, adminJSONRequest(http.MethodGet, "/api/backup-restore-jobs/job-1", ""))
		}()
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"id":"job-1"`) {
			t.Fatalf("httpStatus %d: status=%d body=%s", code, rec.Code, rec.Body.String())
		}
	}
}
