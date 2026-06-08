package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type recordingPrivilegedClient struct {
	serviceActions    []privileged.ServiceActionRequest
	promotions        []privileged.PromoteRequest
	promoteResult     privileged.PromoteResult
	statusRequests    []privileged.ServiceStatusRequest
	statusActiveState string
	journals          []privileged.JournalRequest
	backups           []privileged.BackupRequest
	updates           []privileged.UpdateRequest
	rotateCalls       int
	restartCalls      atomic.Int32
	err               error
}

func (c *recordingPrivilegedClient) Promote(_ context.Context, request privileged.PromoteRequest) (privileged.PromoteResult, error) {
	c.promotions = append(c.promotions, request)
	return c.promoteResult, c.err
}

func TestPrivilegedApplyUsesLogicalArtifactIDsAndOpaqueRollback(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "caddy", "edge.Caddyfile")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingPrivilegedClient{
		statusActiveState: "inactive", // WARP is not running in this caddy-only scenario
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260605T120000.000000000Z",
			WrittenArtifacts: []string{"caddy/edge.Caddyfile"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, Privileged: client})
	context := NewManagementApplyContext(state)
	liveFiles, _, records, err := context.promoteStagedConfigsLocked([]string{staged})
	if err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if !reflect.DeepEqual(client.promotions, []privileged.PromoteRequest{{
		ArtifactIDs: []string{"caddy/edge.Caddyfile"},
	}}) {
		t.Fatalf("promotion requests=%+v", client.promotions)
	}
	if !reflect.DeepEqual(liveFiles, []string{filepath.Join(root, "live", "caddy", "edge.Caddyfile")}) {
		t.Fatalf("live files=%+v", liveFiles)
	}
	if len(records) != 1 || records[0].BackupID != "20260605T120000.000000000Z" {
		t.Fatalf("promotion records=%+v", records)
	}

	client.promoteResult = privileged.PromoteResult{
		BackupID:         "20260605T120000.000000000Z",
		WrittenArtifacts: []string{"caddy/edge.Caddyfile"},
	}
	rollbackFiles, _ := context.rollbackPromotedConfigsLocked(records, liveFiles)
	if len(client.promotions) != 2 || client.promotions[1].RestoreBackupID != "20260605T120000.000000000Z" {
		t.Fatalf("rollback promotions=%+v", client.promotions)
	}
	if !reflect.DeepEqual(rollbackFiles, liveFiles) {
		t.Fatalf("rollback files=%+v", rollbackFiles)
	}
}

func TestPrivilegedApplyHealthChecksUseHelperStatus(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	results := NewManagementApplyContext(state).checkServiceHealthLocked([]ServiceActionResult{{
		Name: "veil-caddy@panel.service", Success: true,
	}})
	if !reflect.DeepEqual(client.statusRequests, []privileged.ServiceStatusRequest{{
		Units: []string{"veil-caddy@panel.service"},
	}}) {
		t.Fatalf("status requests=%+v", client.statusRequests)
	}
	if len(results) != 1 || !results[0].Healthy {
		t.Fatalf("health results=%+v", results)
	}
}

func (c *recordingPrivilegedClient) ServiceAction(_ context.Context, request privileged.ServiceActionRequest) error {
	c.serviceActions = append(c.serviceActions, request)
	return c.err
}

func (c *recordingPrivilegedClient) ServiceStatus(_ context.Context, request privileged.ServiceStatusRequest) (privileged.ServiceStatusResult, error) {
	c.statusRequests = append(c.statusRequests, request)
	if c.err != nil {
		return privileged.ServiceStatusResult{}, c.err
	}
	activeState := c.statusActiveState
	if activeState == "" {
		activeState = "active"
	}
	subState := "running"
	if activeState != "active" {
		subState = "dead"
	}
	result := privileged.ServiceStatusResult{}
	for _, unit := range request.Units {
		result.Services = append(result.Services, privileged.ServiceStatus{
			Unit: unit, LoadState: "loaded", ActiveState: activeState, SubState: subState,
		})
	}
	return result, nil
}

func (c *recordingPrivilegedClient) Journal(_ context.Context, request privileged.JournalRequest) (privileged.JournalResult, error) {
	c.journals = append(c.journals, request)
	return privileged.JournalResult{Unit: request.Unit, Lines: []string{"line one", "line two"}}, c.err
}

func (c *recordingPrivilegedClient) Backup(_ context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	c.backups = append(c.backups, request)
	if c.err != nil {
		return privileged.BackupResult{}, c.err
	}
	switch request.Action {
	case privileged.BackupActionCreate:
		return privileged.BackupResult{ArchiveName: "veil_backup_20260605_120000.tar.gz.enc", Verified: true}, nil
	case privileged.BackupActionList:
		return privileged.BackupResult{Archives: []privileged.BackupArchive{{
			Name: "veil_backup_20260605_120000.tar.gz.enc", Size: 8, Encrypted: true,
		}}}, nil
	case privileged.BackupActionRead:
		return privileged.BackupResult{ArchiveName: request.ArchiveName, Data: []byte("VEILBACK")}, nil
	case privileged.BackupActionVerify:
		return privileged.BackupResult{ArchiveName: request.ArchiveName, Verified: true}, nil
	case privileged.BackupActionPrune:
		return privileged.BackupResult{Pruned: []string{"old.enc"}, Kept: []string{"new.enc"}}, nil
	case privileged.BackupActionRestore:
		return privileged.BackupResult{
			ArchiveName: request.ArchiveName, Verified: true, Restored: true,
			SafetyStatePath: "state.safety", SafetyKeyPath: "key.safety",
		}, nil
	default:
		return privileged.BackupResult{}, errors.New("unexpected backup action")
	}
}

func (c *recordingPrivilegedClient) RotateKey(context.Context, privileged.RotateKeyRequest) error {
	c.rotateCalls++
	return c.err
}

func (c *recordingPrivilegedClient) FirewallApply(context.Context, privileged.FirewallRequest) (privileged.FirewallResult, error) {
	return privileged.FirewallResult{}, c.err
}

func (c *recordingPrivilegedClient) StageUpdate(_ context.Context, request privileged.UpdateRequest) (privileged.UpdateResult, error) {
	c.updates = append(c.updates, request)
	return privileged.UpdateResult{
		ArtifactID: request.ArtifactID, Staged: c.err == nil, Installed: c.err == nil, Version: request.Version,
	}, c.err
}

func (c *recordingPrivilegedClient) RestartPanel(context.Context) error {
	c.restartCalls.Add(1)
	return c.err
}

func TestPrivilegedServiceStatusAndLogsUseManagedUnits(t *testing.T) {
	client := &recordingPrivilegedClient{}
	router, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", Privileged: client})

	restart := httptest.NewRequest(http.MethodPost, "/api/services/caddy-panel/restart", strings.NewReader(`{"confirm":true}`))
	restart.Header.Set("Content-Type", "application/json")
	restartResponse := httptest.NewRecorder()
	router.ServeHTTP(restartResponse, restart)
	if restartResponse.Code != http.StatusOK {
		t.Fatalf("restart status=%d body=%s", restartResponse.Code, restartResponse.Body.String())
	}
	wantAction := privileged.ServiceActionRequest{
		Unit: "veil-caddy@panel.service", Action: privileged.ServiceActionRestart,
	}
	if !reflect.DeepEqual(client.serviceActions, []privileged.ServiceActionRequest{wantAction}) {
		t.Fatalf("service actions=%+v", client.serviceActions)
	}

	statusResponse := httptest.NewRecorder()
	router.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if statusResponse.Code != http.StatusOK || len(client.statusRequests) != 1 {
		t.Fatalf("status=%d requests=%+v", statusResponse.Code, client.statusRequests)
	}
	for _, unit := range client.statusRequests[0].Units {
		if !strings.HasSuffix(unit, ".service") {
			t.Fatalf("status received non-unit %q", unit)
		}
	}

	logResponse := httptest.NewRecorder()
	router.ServeHTTP(logResponse, httptest.NewRequest(http.MethodGet, "/api/logs?unit=caddy-panel&lines=500", nil))
	if logResponse.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", logResponse.Code, logResponse.Body.String())
	}
	if !reflect.DeepEqual(client.journals, []privileged.JournalRequest{{
		Unit: "veil-caddy@panel.service", Lines: 500,
	}}) {
		t.Fatalf("journal requests=%+v", client.journals)
	}
}

func TestPrivilegedBackupRoutesNeverReadHTTPPassphrase(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newManagementState(ServerInfo{
		Version:    "0.6.0",
		Mode:       "dev",
		StatePath:  filepath.Join(t.TempDir(), "state.json"),
		KeyPath:    filepath.Join(t.TempDir(), "state.key"),
		ApplyRoot:  t.TempDir(),
		Privileged: client,
	})

	createResponse := httptest.NewRecorder()
	state.handleBackups(createResponse, adminJSONRequest(http.MethodPost, "/api/backups", `{"prune":false}`))
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	if len(client.backups) != 1 || client.backups[0].Action != privileged.BackupActionCreate {
		t.Fatalf("backup requests=%+v", client.backups)
	}

	downloadResponse := httptest.NewRecorder()
	state.handleBackupByName(downloadResponse, adminJSONRequest(
		http.MethodGet,
		"/api/backups/veil_backup_20260605_120000.tar.gz.enc/download",
		"",
	))
	if downloadResponse.Code != http.StatusOK || downloadResponse.Body.String() != "VEILBACK" {
		t.Fatalf("download status=%d body=%q", downloadResponse.Code, downloadResponse.Body.String())
	}
	if client.backups[len(client.backups)-1].Action != privileged.BackupActionRead {
		t.Fatalf("download request=%+v", client.backups[len(client.backups)-1])
	}
}

func TestPrivilegedUpdateStagesArtifactAndRestartsPanel(t *testing.T) {
	client := &recordingPrivilegedClient{}
	routes := PanelRoutes{
		Info: ServerInfo{Version: "0.6.0"},
		State: newManagementState(ServerInfo{
			Mode: "dev", Privileged: client,
			UpdateStager: func(context.Context) (string, error) { return "v0.6.0", nil },
		}),
	}
	response := httptest.NewRecorder()
	routes.handleUpdateVersion(response, httptest.NewRequest(http.MethodPost, "/api/version/update", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(client.updates, []privileged.UpdateRequest{{ArtifactID: "veil-update", Version: "v0.6.0"}}) {
		t.Fatalf("updates=%+v", client.updates)
	}
	deadline := time.Now().Add(time.Second)
	for client.restartCalls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if client.restartCalls.Load() != 1 {
		t.Fatalf("restart calls=%d", client.restartCalls.Load())
	}
}

func TestPrivilegedUpdateUnavailableTellsOperatorHowToRepair(t *testing.T) {
	routes := PanelRoutes{
		Info: ServerInfo{Version: "0.6.0"},
		State: newManagementState(ServerInfo{
			Mode:                    "dev",
			RequirePrivilegedHelper: true,
			UpdateStager: func(context.Context) (string, error) {
				t.Fatal("update should fail before staging when helper is unavailable")
				return "", nil
			},
		}),
	}
	response := httptest.NewRecorder()
	routes.handleUpdateVersion(response, httptest.NewRequest(http.MethodPost, "/api/version/update", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{"privileged helper is unavailable", "sudo /usr/local/bin/veil repair --yes", "veil-helper.socket", "install.sh --force"} {
		if !strings.Contains(body, want) {
			t.Fatalf("helper-unavailable response missing %q:\n%s", want, body)
		}
	}
}

func TestMissingPrivilegedHelperSocketTellsOperatorHowToRepair(t *testing.T) {
	client := &recordingPrivilegedClient{
		err: &privileged.Error{
			Code:    privileged.ErrorOperationFailed,
			Message: "privileged operation failed: dial unix /run/veil/helper.sock: connect: no such file or directory",
		},
	}
	router, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", Privileged: client})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{"privileged helper is unavailable", "veil-helper.socket", "install.sh --force"} {
		if !strings.Contains(body, want) {
			t.Fatalf("helper socket response missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "dial unix") {
		t.Fatalf("helper socket response should not expose raw dial error:\n%s", body)
	}
}

func TestRotateKeyRequiresAdminAndRevokesSessions(t *testing.T) {
	client := &recordingPrivilegedClient{}
	root := t.TempDir()
	state := newManagementState(ServerInfo{
		Mode: "dev", Privileged: client,
		StatePath: filepath.Join(root, "state.json"),
		KeyPath:   filepath.Join(root, "state.key"),
	})
	admin, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin"})
	viewer, _ := state.sessionRegistry().Create(SessionCreateInput{Username: "viewer", Role: "viewer"})

	forbidden := httptest.NewRecorder()
	state.handleRotateKey(forbidden, httptest.NewRequest(http.MethodPost, "/api/admin/rotate-key", nil))
	if forbidden.Code != http.StatusForbidden || client.rotateCalls != 0 {
		t.Fatalf("viewer status=%d rotateCalls=%d", forbidden.Code, client.rotateCalls)
	}

	request := adminJSONRequest(http.MethodPost, "/api/admin/rotate-key", `{}`)
	request.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	response := httptest.NewRecorder()
	state.handleRotateKey(response, request)
	if response.Code != http.StatusOK || client.rotateCalls != 1 {
		t.Fatalf("rotate status=%d calls=%d body=%s", response.Code, client.rotateCalls, response.Body.String())
	}
	if _, ok := state.sessionRegistry().Get(viewer.Token); ok {
		t.Fatal("key rotation did not revoke viewer session")
	}
}

func TestPrivilegedErrorsUseStructuredEnvelope(t *testing.T) {
	client := &recordingPrivilegedClient{
		err: &privileged.Error{Code: privileged.ErrorForbiddenOperation, Message: "denied"},
	}
	router, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", Privileged: client})
	request := httptest.NewRequest(http.MethodPost, "/api/services/veil/restart", strings.NewReader(`{"confirm":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode structured error: %v body=%s", err, response.Body.String())
	}
	if body.Error.Code != string(privileged.ErrorForbiddenOperation) || body.Error.Message != "denied" {
		t.Fatalf("error body=%+v", body)
	}
}
