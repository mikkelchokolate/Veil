package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
)

func TestBackupCreateRejectsInvalidRetentionBeforeCreatingArchive(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "negative daily", body: `{"prune":true,"daily":-1,"weekly":4,"monthly":12}`, want: "daily retention"},
		{name: "excessive weekly", body: `{"prune":true,"daily":7,"weekly":105,"monthly":12}`, want: "weekly retention"},
		{name: "excessive monthly", body: `{"prune":true,"daily":7,"weekly":4,"monthly":121}`, want: "monthly retention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newPanelBackupState(t)
			request := adminJSONRequest(http.MethodPost, "/api/backups", tc.body)
			response := httptest.NewRecorder()
			state.handleBackups(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}

			list := adminJSONRequest(http.MethodGet, "/api/backups", "")
			listResponse := httptest.NewRecorder()
			state.handleBackups(listResponse, list)
			var archives []backup.ArchiveEntry
			if err := json.NewDecoder(listResponse.Body).Decode(&archives); err != nil {
				t.Fatal(err)
			}
			if len(archives) != 0 {
				t.Fatalf("invalid retention created archives: %+v", archives)
			}
		})
	}
}

func TestBackupPruneRejectsInvalidRetention(t *testing.T) {
	state := newPanelBackupState(t)
	request := adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"daily":7,"weekly":-1,"monthly":12}`)
	response := httptest.NewRecorder()
	state.handleBackupPrune(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "weekly retention") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupRestoreSerializesMutatingBackupOperations(t *testing.T) {
	state := newPanelBackupState(t)
	state.backupRestoreOwnerSessionGrace = -1
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	auditStarted := make(chan audit.Record, 1)
	releaseAudit := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseAudit) }) }
	defer release()
	state.backupRestoreAudit = func(record audit.Record) error {
		auditStarted <- record
		<-releaseAudit
		return nil
	}
	owner, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "owner", Role: "admin"})
	first := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/restore", `{"confirm":true}`)
	first.AddCookie(&http.Cookie{Name: "veil_session", Value: owner.Token})
	firstResponse := httptest.NewRecorder()
	state.handleBackupByName(firstResponse, first)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first restore status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	var accepted BackupRestoreJob
	if err := json.NewDecoder(firstResponse.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}

	select {
	case <-auditStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("restore did not reach audit finalization")
	}

	second := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/restore", `{"confirm":true}`)
	secondResponse := httptest.NewRecorder()
	state.handleBackupByName(secondResponse, second)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("second restore status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}

	createDuringRestore := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createDuringRestoreResponse := httptest.NewRecorder()
	state.handleBackups(createDuringRestoreResponse, createDuringRestore)
	if createDuringRestoreResponse.Code != http.StatusConflict {
		t.Fatalf("create during restore status=%d body=%s", createDuringRestoreResponse.Code, createDuringRestoreResponse.Body.String())
	}

	pruneDuringRestore := adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"daily":7,"weekly":4,"monthly":12}`)
	pruneDuringRestoreResponse := httptest.NewRecorder()
	state.handleBackupPrune(pruneDuringRestoreResponse, pruneDuringRestore)
	if pruneDuringRestoreResponse.Code != http.StatusConflict {
		t.Fatalf("prune during restore status=%d body=%s", pruneDuringRestoreResponse.Code, pruneDuringRestoreResponse.Body.String())
	}

	release()
	waitForBackupRestoreTerminalState(t, state, accepted.ID)
}

func TestBackupRestoreImmediatelyRevokesOwnerMissingFromRestoredUsers(t *testing.T) {
	state := newPanelBackupState(t)
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	owner, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "owner", Role: "admin"})
	restore := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/restore", `{"confirm":true}`)
	restore.AddCookie(&http.Cookie{Name: "veil_session", Value: owner.Token})
	restoreResponse := httptest.NewRecorder()
	state.handleBackupByName(restoreResponse, restore)
	if restoreResponse.Code != http.StatusAccepted {
		t.Fatalf("restore status=%d body=%s", restoreResponse.Code, restoreResponse.Body.String())
	}
	var accepted BackupRestoreJob
	if err := json.NewDecoder(restoreResponse.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	waitForBackupRestoreTerminalState(t, state, accepted.ID)
	if _, ok := state.sessionRegistry().Get(owner.Token); ok {
		t.Fatal("owner missing from restored users retained an obsolete admin session")
	}
	job, ok := state.backupRestoreJob(accepted.ID)
	if !ok || job.ownerSessionToken != "" {
		t.Fatalf("restore job retained revoked owner session token: %+v", job)
	}
}

func waitForBackupRestoreTerminalState(t *testing.T, state *managementState, id string) BackupRestoreJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, ok := state.backupRestoreJob(id)
		if !ok {
			t.Fatal("restore job disappeared")
		}
		if job.Status == "succeeded" {
			return job
		}
		if job.Status == "failed" {
			t.Fatalf("restore job failed: %+v", job)
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore job timed out: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
