package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestBackupRoutesCreateListVerifyAndDownload(t *testing.T) {
	state := newPanelBackupState(t)
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{"prune":false}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Archive.Name == "" || !created.Verification.Encrypted {
		t.Fatalf("created=%+v", created)
	}

	list := adminJSONRequest(http.MethodGet, "/api/backups", "")
	listResponse := httptest.NewRecorder()
	state.handleBackups(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var archives []backup.ArchiveEntry
	if err := json.NewDecoder(listResponse.Body).Decode(&archives); err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 || archives[0].Name != created.Archive.Name {
		t.Fatalf("archives=%+v", archives)
	}

	verify := adminJSONRequest(http.MethodPost, "/api/backups/"+created.Archive.Name+"/verify", `{}`)
	verifyResponse := httptest.NewRecorder()
	state.handleBackupByName(verifyResponse, verify)
	if verifyResponse.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyResponse.Code, verifyResponse.Body.String())
	}

	download := adminJSONRequest(http.MethodGet, "/api/backups/"+created.Archive.Name+"/download", "")
	downloadResponse := httptest.NewRecorder()
	state.handleBackupByName(downloadResponse, download)
	if downloadResponse.Code != http.StatusOK ||
		downloadResponse.Header().Get("Content-Disposition") == "" ||
		!bytes.HasPrefix(downloadResponse.Body.Bytes(), []byte("VEILBACK")) {
		t.Fatalf("download status=%d headers=%v", downloadResponse.Code, downloadResponse.Header())
	}

	remove := adminJSONRequest(http.MethodDelete, "/api/backups/"+created.Archive.Name, "")
	removeResponse := httptest.NewRecorder()
	state.handleBackupByName(removeResponse, remove)
	if removeResponse.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", removeResponse.Code, removeResponse.Body.String())
	}
}

func TestBackupRoutesRequireAdminAndServerSidePassphrase(t *testing.T) {
	state := newPanelBackupState(t)
	viewer := httptest.NewRequest(http.MethodGet, "/api/backups", nil)
	viewer = viewer.WithContext(context.WithValue(viewer.Context(), contextKeyRole, "viewer"))
	viewerResponse := httptest.NewRecorder()
	state.handleBackups(viewerResponse, viewer)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", viewerResponse.Code)
	}

	if err := os.Remove(state.backupPassphrasePath); err != nil {
		t.Fatal(err)
	}
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("missing passphrase status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	if _, err := os.Stat(state.backupPassphrasePath); err != nil {
		t.Fatalf("create should write a backup passphrase: %v", err)
	}
}

func TestBackupRoutesRejectShortPassphraseWithPublicMessage(t *testing.T) {
	state := newPanelBackupState(t)
	if err := os.WriteFile(state.backupPassphrasePath, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("short passphrase status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	if !strings.Contains(createResponse.Body.String(), privileged.MessageBackupPassphraseTooShort) {
		t.Fatalf("short passphrase body=%s", createResponse.Body.String())
	}
}

func TestBackupRoutesRejectTraversalAndPruneManagedArchives(t *testing.T) {
	state := newPanelBackupState(t)
	traversal := adminJSONRequest(http.MethodGet, "/api/backups/..%2Fstate.json/download", "")
	traversalResponse := httptest.NewRecorder()
	state.handleBackupByName(traversalResponse, traversal)
	if traversalResponse.Code != http.StatusBadRequest {
		t.Fatalf("traversal status=%d body=%s", traversalResponse.Code, traversalResponse.Body.String())
	}

	for _, name := range []string{
		"veil_backup_20240101_020000.tar.gz.enc",
		"veil_backup_20240201_020000.tar.gz.enc",
	} {
		if err := os.WriteFile(filepath.Join(state.backupDir, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prune := adminJSONRequest(http.MethodPost, "/api/backups/prune", `{"daily":1,"weekly":0,"monthly":0}`)
	pruneResponse := httptest.NewRecorder()
	state.handleBackupPrune(pruneResponse, prune)
	if pruneResponse.Code != http.StatusOK {
		t.Fatalf("prune status=%d body=%s", pruneResponse.Code, pruneResponse.Body.String())
	}
	var result backup.PruneResult
	if err := json.NewDecoder(pruneResponse.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("prune result=%+v", result)
	}
}

func TestBackupRestoreRunsAsQueuedJobAndRevokesSessions(t *testing.T) {
	stubManagementApplySideEffects(t)
	state := newPanelBackupState(t)
	collectorBeforeRestore := state.trafficCollector
	reconcilerBeforeRestore := state.trafficReconciler
	auditStarted := make(chan audit.Record, 1)
	releaseAudit := make(chan struct{})
	var releaseAuditOnce sync.Once
	releaseAuditFinalization := func() {
		releaseAuditOnce.Do(func() { close(releaseAudit) })
	}
	t.Cleanup(releaseAuditFinalization)
	state.backupRestoreAudit = func(record audit.Record) error {
		auditStarted <- record
		<-releaseAudit
		return nil
	}
	for _, body := range []string{
		`{"username":"alice","password":"alice-password-123","role":"viewer"}`,
		`{"username":"bob","password":"bob-password-12345","role":"viewer"}`,
	} {
		request := adminJSONRequest(http.MethodPost, "/api/users", body)
		response := httptest.NewRecorder()
		state.handleUsersRoute(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("seed restored user status=%d body=%s", response.Code, response.Body.String())
		}
	}
	create := adminJSONRequest(http.MethodPost, "/api/backups", `{}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	ownerSession, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "alice", Role: "admin"})
	otherSession, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "bob", Role: "viewer"})

	restore := adminJSONRequest(
		http.MethodPost,
		"/api/backups/"+created.Archive.Name+"/restore",
		`{"confirm":true}`,
	)
	restore.AddCookie(&http.Cookie{Name: "veil_session", Value: ownerSession.Token})
	restoreResponse := httptest.NewRecorder()
	state.handleBackupByName(restoreResponse, restore)
	if restoreResponse.Code != http.StatusAccepted {
		t.Fatalf("restore status=%d body=%s", restoreResponse.Code, restoreResponse.Body.String())
	}
	var accepted BackupRestoreJob
	if err := json.NewDecoder(restoreResponse.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-auditStarted:
		if record.Action != "backup.restore" {
			t.Fatalf("unexpected restore audit record: %+v", record)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("restore audit finalization did not start")
	}
	if job, _ := state.backupRestoreJob(accepted.ID); job.Status == "succeeded" {
		t.Fatal("restore job reported success before audit finalization completed")
	}
	releaseAuditFinalization()
	deadline := time.Now().Add(30 * time.Second)
	for {
		job, ok := state.backupRestoreJob(accepted.ID)
		if !ok {
			t.Fatal("restore job disappeared")
		}
		if job.Status == "succeeded" {
			break
		}
		if job.Status == "failed" {
			t.Fatalf("restore job failed: %+v", job)
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore job timed out: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	completed, _ := state.backupRestoreJob(accepted.ID)
	if completed.SafetyStatePath == "" || completed.SafetyKeyPath == "" || completed.SafetyDatabasePath == "" {
		t.Fatalf("restore job missing safety paths: %+v", completed)
	}
	if state.db == nil || state.db.Ping() != nil || state.clientRepo == nil {
		t.Fatal("restore job did not reopen SQLite-backed subsystems")
	}
	if collectorBeforeRestore.Running() || reconcilerBeforeRestore.Running() {
		t.Fatal("restore returned before old traffic workers stopped")
	}
	if state.trafficCollector == nil || state.trafficReconciler == nil ||
		state.trafficCollector == collectorBeforeRestore || state.trafficReconciler == reconcilerBeforeRestore ||
		!state.trafficCollector.Running() || !state.trafficReconciler.Running() {
		t.Fatal("restore did not create exactly one live worker pair for the reopened database")
	}
	if _, ok := state.sessionRegistry().Get(ownerSession.Token); !ok {
		t.Fatal("restore revoked owner session before final status poll")
	}
	if _, ok := state.sessionRegistry().Get(otherSession.Token); ok {
		t.Fatal("restore did not revoke another active session")
	}
	status := httptest.NewRequest(http.MethodGet, "/api/backup-restore-jobs/"+accepted.ID, nil)
	status = status.WithContext(context.WithValue(status.Context(), contextKeyRole, "viewer"))
	status.AddCookie(&http.Cookie{Name: "veil_session", Value: ownerSession.Token})
	statusResponse := httptest.NewRecorder()
	state.handleBackupRestoreJob(statusResponse, status)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("job status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(ownerSession.Token); ok {
		t.Fatal("restore did not revoke owner session after final status poll")
	}
}

func newPanelBackupState(t *testing.T) *managementState {
	t.Helper()
	dir := t.TempDir()
	info := ServerInfo{
		Version:   "0.6.0",
		Mode:      "server",
		StatePath: filepath.Join(dir, "state.json"),
		KeyPath:   filepath.Join(dir, "state.key"),
		ApplyRoot: filepath.Join(dir, "etc"),
	}
	state := newManagementState(info)
	t.Cleanup(func() {
		if err := state.Close(); err != nil {
			t.Errorf("close backup test state: %v", err)
		}
	})
	state.mu.Lock()
	if err := state.saveLocked(); err != nil {
		state.mu.Unlock()
		t.Fatal(err)
	}
	state.mu.Unlock()
	if err := os.MkdirAll(state.backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.backupPassphrasePath, []byte("panel-backup-passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

func adminJSONRequest(method, path, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	return request.WithContext(context.WithValue(request.Context(), contextKeyRole, "admin"))
}
