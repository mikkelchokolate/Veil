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

	"github.com/mikkelchokolate/Veil/internal/backupsftp"
	"github.com/mikkelchokolate/Veil/internal/testutil/sftpfake"
)

// stubSftpDial swaps the package-level dial seam (the production executor in
// the local adapter leaves SftpDial nil, which resolves backupsftp.Dial at
// call time) so API tests exercise the full route → adapter → engine chain
// against an in-memory remote.
func stubSftpDial(t *testing.T, remote *sftpfake.MemFS, dialErr error) {
	t.Helper()
	original := backupsftp.Dial
	backupsftp.Dial = func(context.Context, backupsftp.Config, string) (backupsftp.RemoteFS, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return remote, nil
	}
	t.Cleanup(func() { backupsftp.Dial = original })
}

// sftpTestInstallID pins the per-installation remote namespace identity the
// engine resolves under the panel state dir (#1184), so tests can seed
// deterministic remote paths.
const sftpTestInstallID = "0123456789abcdef0123456789abcdef"

// sftpTestRemoteDir is this node's remote namespace under remoteDir.
const sftpTestRemoteDir = "/srv/veil-backups/veil-node-" + sftpTestInstallID

func seedSftpInstallID(t *testing.T, state *managementState) {
	t.Helper()
	path := filepath.Join(filepath.Dir(state.statePath), backupsftp.InstallIDFileName)
	if err := os.WriteFile(path, []byte(sftpTestInstallID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupSftpRouteRequiresAdmin(t *testing.T) {
	state := newPanelBackupState(t)
	viewer := httptest.NewRequest(http.MethodGet, "/api/backups/sftp", nil)
	viewer = viewer.WithContext(context.WithValue(viewer.Context(), contextKeyRole, "viewer"))
	response := httptest.NewRecorder()
	state.handleBackupSftp(response, viewer)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", response.Code)
	}
	viewer = httptest.NewRequest(http.MethodGet, "/api/backups/sftp/remote", nil)
	viewer = viewer.WithContext(context.WithValue(viewer.Context(), contextKeyRole, "viewer"))
	response = httptest.NewRecorder()
	state.handleBackupSftpRemote(response, viewer)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer remote status=%d", response.Code)
	}
}

func TestBackupSftpGetWhenUnconfigured(t *testing.T) {
	state := newPanelBackupState(t)
	request := adminJSONRequest(http.MethodGet, "/api/backups/sftp", "")
	response := httptest.NewRecorder()
	state.handleBackupSftp(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", response.Code, response.Body.String())
	}
	var view BackupSftpDestinationResponse
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Configured {
		t.Fatalf("unconfigured view=%+v", view)
	}
}

func TestBackupSftpPutGetDeleteAndSecretRedaction(t *testing.T) {
	stubSftpDial(t, sftpfake.New(), nil)
	state := newPanelBackupState(t)
	const secret = "super-secret-sftp-password"

	put := adminJSONRequest(http.MethodPut, "/api/backups/sftp", `{
		"enabled": true, "host": "backups.example.com", "port": 2222,
		"user": "veil", "remoteDir": "/srv/veil-backups",
		"authType": "password", "password": "`+secret+`"
	}`)
	putResponse := httptest.NewRecorder()
	state.handleBackupSftp(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	if strings.Contains(putResponse.Body.String(), secret) {
		t.Fatalf("PUT response leaked the password: %s", putResponse.Body.String())
	}
	var view BackupSftpDestinationResponse
	if err := json.NewDecoder(putResponse.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !view.Configured || !view.Enabled || !view.PasswordSet ||
		view.Host != "backups.example.com" || view.Port != 2222 {
		t.Fatalf("view=%+v", view)
	}

	// The secret must persist in the root-only file under the etc root.
	configPath := filepath.Join(filepath.Dir(state.liveRoot), "backup-sftp.json")
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), secret) {
		t.Fatal("secret not persisted in config file")
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}

	// GET returns flags, never the value.
	get := adminJSONRequest(http.MethodGet, "/api/backups/sftp", "")
	getResponse := httptest.NewRecorder()
	state.handleBackupSftp(getResponse, get)
	if getResponse.Code != http.StatusOK ||
		strings.Contains(getResponse.Body.String(), secret) {
		t.Fatalf("GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	var getView BackupSftpDestinationResponse
	if err := json.NewDecoder(getResponse.Body).Decode(&getView); err != nil {
		t.Fatal(err)
	}
	if !getView.PasswordSet || !getView.Configured {
		t.Fatalf("GET view=%+v", getView)
	}

	// PUT without a password preserves the stored secret.
	put2 := adminJSONRequest(http.MethodPut, "/api/backups/sftp", `{
		"enabled": true, "host": "backups.example.com", "port": 2222,
		"user": "veil", "remoteDir": "/srv/other", "authType": "password"
	}`)
	put2Response := httptest.NewRecorder()
	state.handleBackupSftp(put2Response, put2)
	if put2Response.Code != http.StatusOK {
		t.Fatalf("put2 status=%d body=%s", put2Response.Code, put2Response.Body.String())
	}
	persisted, err := backupsftp.LoadConfig(configPath)
	if err != nil || persisted.Password != secret || persisted.RemoteDir != "/srv/other" {
		t.Fatalf("secret lost on update: %v %+v", err, persisted)
	}

	// DELETE clears the destination.
	del := adminJSONRequest(http.MethodDelete, "/api/backups/sftp", "")
	delResponse := httptest.NewRecorder()
	state.handleBackupSftp(delResponse, del)
	if delResponse.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delResponse.Code, delResponse.Body.String())
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config still present: %v", err)
	}

	// Audit recorded the configure + delete without any secret material.
	records, err := state.auditRecorder().List(20, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var sawConfigure, sawDelete bool
	for _, record := range records {
		switch record.Action {
		case "backup.sftp.configure":
			sawConfigure = true
		case "backup.sftp.delete":
			sawDelete = true
		}
		encoded, _ := json.Marshal(record)
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("audit record leaked secret: %+v", record)
		}
	}
	if !sawConfigure || !sawDelete {
		t.Fatalf("audit records missing configure/delete")
	}
}

func TestBackupSftpPutRejectsInvalidConfig(t *testing.T) {
	state := newPanelBackupState(t)
	put := adminJSONRequest(http.MethodPut, "/api/backups/sftp", `{
		"enabled": true, "host": "", "user": "veil",
		"remoteDir": "/srv/veil-backups", "authType": "password", "password": "pw"
	}`)
	response := httptest.NewRecorder()
	state.handleBackupSftp(response, put)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid put status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupSftpRemoteListAndFetch(t *testing.T) {
	remote := sftpfake.New()
	stubSftpDial(t, remote, nil)
	state := newPanelBackupState(t)

	// Configure the destination first.
	put := adminJSONRequest(http.MethodPut, "/api/backups/sftp", `{
		"enabled": true, "host": "backups.example.com",
		"user": "veil", "remoteDir": "/srv/veil-backups",
		"authType": "password", "password": "pw"
	}`)
	putResponse := httptest.NewRecorder()
	state.handleBackupSftp(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	seedSftpInstallID(t, state)
	name := "veil_backup_20260101_020000.tar.gz.enc"
	remote.SetFile(sftpTestRemoteDir+"/"+name, []byte("remote-archive"))
	// A foreign archive outside this node's namespace must never be listed
	// or fetchable (#1184).
	remote.SetFile("/srv/veil-backups/veil_backup_20260102_020000.tar.gz.enc", []byte("foreign"))

	list := adminJSONRequest(http.MethodGet, "/api/backups/sftp/remote", "")
	listResponse := httptest.NewRecorder()
	state.handleBackupSftpRemote(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("remote list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var entries []map[string]any
	if err := json.NewDecoder(listResponse.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0]["name"] != name {
		t.Fatalf("entries=%v", entries)
	}

	fetch := adminJSONRequest(http.MethodPost, "/api/backups/sftp/fetch", `{"name":"`+name+`"}`)
	fetchResponse := httptest.NewRecorder()
	state.handleBackupSftpRemote(fetchResponse, fetch)
	if fetchResponse.Code != http.StatusOK {
		t.Fatalf("fetch status=%d body=%s", fetchResponse.Code, fetchResponse.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(state.backupDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "remote-archive" {
		t.Fatalf("fetched=%q", got)
	}

	// The fetched archive is a normal local archive now.
	listLocal := adminJSONRequest(http.MethodGet, "/api/backups", "")
	listLocalResponse := httptest.NewRecorder()
	state.handleBackups(listLocalResponse, listLocal)
	if !strings.Contains(listLocalResponse.Body.String(), name) {
		t.Fatalf("local list=%s", listLocalResponse.Body.String())
	}

	// Fetch rejects a name that already exists locally rather than
	// overwriting it.
	fetchAgain := adminJSONRequest(http.MethodPost, "/api/backups/sftp/fetch", `{"name":"`+name+`"}`)
	againResponse := httptest.NewRecorder()
	state.handleBackupSftpRemote(againResponse, fetchAgain)
	if againResponse.Code == http.StatusOK {
		t.Fatalf("refetch succeeded: %s", againResponse.Body.String())
	}

	// Traversal must be rejected before any remote call.
	fetchBad := adminJSONRequest(http.MethodPost, "/api/backups/sftp/fetch", `{"name":"../evil.enc"}`)
	badResponse := httptest.NewRecorder()
	state.handleBackupSftpRemote(badResponse, fetchBad)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("traversal status=%d body=%s", badResponse.Code, badResponse.Body.String())
	}
}

func TestBackupSftpRemoteListUnconfiguredFailsCleanly(t *testing.T) {
	stubSftpDial(t, sftpfake.New(), nil)
	state := newPanelBackupState(t)
	request := adminJSONRequest(http.MethodGet, "/api/backups/sftp/remote", "")
	response := httptest.NewRecorder()
	state.handleBackupSftpRemote(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("list without config succeeded: %s", response.Body.String())
	}
}

func TestBackupCreateSurfacesRemoteUploadOutcome(t *testing.T) {
	remote := sftpfake.New()
	stubSftpDial(t, remote, nil)
	state := newPanelBackupState(t)
	seedSftpInstallID(t, state)

	put := adminJSONRequest(http.MethodPut, "/api/backups/sftp", `{
		"enabled": true, "host": "backups.example.com",
		"user": "veil", "remoteDir": "/srv/veil-backups",
		"authType": "password", "password": "pw"
	}`)
	putResponse := httptest.NewRecorder()
	state.handleBackupSftp(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put: %s", putResponse.Body.String())
	}

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
	if created.Remote == nil || created.Remote.Upload == nil ||
		created.Remote.Upload.Archive != created.Archive.Name || created.Remote.Error != "" {
		t.Fatalf("remote result=%+v", created.Remote)
	}
	if !remote.Has(sftpTestRemoteDir + "/" + created.Archive.Name) {
		t.Fatalf("remote missing upload: %v", remote.Paths())
	}
}

func TestBackupCreateRemoteFailureIsNonFatalAndAudited(t *testing.T) {
	stubSftpDial(t, sftpfake.New(), os.ErrPermission)
	state := newPanelBackupState(t)
	put := adminJSONRequest(http.MethodPut, "/api/backups/sftp", `{
		"enabled": true, "host": "backups.example.com",
		"user": "veil", "remoteDir": "/srv/veil-backups",
		"authType": "password", "password": "pw"
	}`)
	putResponse := httptest.NewRecorder()
	state.handleBackupSftp(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put: %s", putResponse.Body.String())
	}

	create := adminJSONRequest(http.MethodPost, "/api/backups", `{"prune":false}`)
	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create must succeed despite remote failure: %d %s", createResponse.Code, createResponse.Body.String())
	}
	var created BackupCreateResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Remote == nil || created.Remote.Error == "" ||
		!strings.Contains(created.Warning, "sftp upload failed") {
		t.Fatalf("remote failure not loud: %+v", created)
	}
	if created.Archive.Name == "" {
		t.Fatal("local archive missing")
	}
	records, err := state.auditRecorder().List(20, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	sawRemoteError := false
	for _, record := range records {
		if record.Action == "backup.create" {
			if detail, ok := record.Details["remoteError"].(string); ok && detail != "" {
				sawRemoteError = true
			}
		}
	}
	if !sawRemoteError {
		t.Fatal("audit missing remoteError detail on backup.create")
	}
}
