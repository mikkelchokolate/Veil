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
	serviceActions        []privileged.ServiceActionRequest
	promotions            []privileged.PromoteRequest
	promoteResult         privileged.PromoteResult
	statusRequests        []privileged.ServiceStatusRequest
	statusActiveState     string
	statusActiveStates    []string
	journals              []privileged.JournalRequest
	journalLines          []string
	backups               []privileged.BackupRequest
	updates               []privileged.UpdateRequest
	syncCaddyCertRequests []privileged.SyncCaddyCertRequest
	rotateCalls           int
	recoverRotationCalls  int
	restartCalls          atomic.Int32
	restartErr            error
	err                   error
}

func (c *recordingPrivilegedClient) Promote(_ context.Context, request privileged.PromoteRequest) (privileged.PromoteResult, error) {
	c.promotions = append(c.promotions, request)
	return c.promoteResult, c.err
}

func TestPrivilegedApplyUsesLogicalArtifactIDsAndOpaqueRollback(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "caddy", "config.json")
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
			WrittenArtifacts: []string{"caddy/config.json"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, Privileged: client})
	context := NewManagementApplyContext(state)
	liveFiles, _, records, err := context.promoteStagedConfigs([]string{staged})
	if err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if !reflect.DeepEqual(client.promotions, []privileged.PromoteRequest{{
		ArtifactIDs: []string{"caddy/config.json"},
	}}) {
		t.Fatalf("promotion requests=%+v", client.promotions)
	}
	if !reflect.DeepEqual(liveFiles, []string{filepath.Join(root, "live", "caddy", "config.json")}) {
		t.Fatalf("live files=%+v", liveFiles)
	}
	if len(records) != 1 || records[0].BackupID != "20260605T120000.000000000Z" {
		t.Fatalf("promotion records=%+v", records)
	}

	client.promoteResult = privileged.PromoteResult{
		BackupID:         "20260605T120000.000000000Z",
		WrittenArtifacts: []string{"caddy/config.json"},
	}
	rollbackFiles, _ := context.rollbackPromotedConfigs(records, liveFiles)
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
	results := NewManagementApplyContext(state).checkServiceHealth([]ServiceActionResult{{
		Name: "veil-caddy.service", Command: []string{"systemctl", "reload", "veil-caddy.service"}, Success: true,
	}})
	if !reflect.DeepEqual(client.statusRequests, []privileged.ServiceStatusRequest{{
		Units: []string{"veil-caddy.service"},
	}}) {
		t.Fatalf("status requests=%+v", client.statusRequests)
	}
	if len(results) != 1 || !results[0].Healthy {
		t.Fatalf("health results=%+v", results)
	}
}

func stubServiceHealthTiming(t *testing.T, interval, timeout, window time.Duration) {
	t.Helper()
	oldInterval, oldTimeout, oldWindow := serviceHealthPollInterval, serviceHealthPollTimeout, serviceHealthStableWindow
	serviceHealthPollInterval = interval
	serviceHealthPollTimeout = timeout
	serviceHealthStableWindow = window
	t.Cleanup(func() {
		serviceHealthPollInterval = oldInterval
		serviceHealthPollTimeout = oldTimeout
		serviceHealthStableWindow = oldWindow
	})
}

func TestPrivilegedApplyHealthChecksWaitForActivatingService(t *testing.T) {
	stubServiceHealthTiming(t, time.Millisecond, time.Second, 0)

	client := &recordingPrivilegedClient{statusActiveStates: []string{"activating", "active"}}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	results := NewManagementApplyContext(state).checkServiceHealth([]ServiceActionResult{{
		Name: "veil-hysteria2@edge.service", Command: []string{"systemctl", "restart", "veil-hysteria2@edge.service"}, Success: true,
	}})
	if len(client.statusRequests) != 3 {
		t.Fatalf("status requests = %d, want 3", len(client.statusRequests))
	}
	if len(results) != 1 || !results[0].Healthy || results[0].Output != "active/running" {
		t.Fatalf("health results=%+v", results)
	}
}

func TestPrivilegedApplyHealthChecksIgnoreStoppedOrphans(t *testing.T) {
	stubServiceHealthTiming(t, time.Millisecond, time.Second, 0)
	client := &recordingPrivilegedClient{}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	results := NewManagementApplyContext(state).checkServiceHealth([]ServiceActionResult{
		{Name: "veil-hysteria2@new.service", Command: []string{"systemctl", "restart", "veil-hysteria2@new.service"}, Success: true},
		{Name: "veil-hysteria2@old.service", Command: []string{"systemctl", "stop", "veil-hysteria2@old.service"}, Success: true},
		{Name: "veil-hysteria2@old.service", Command: []string{"systemctl", "disable", "veil-hysteria2@old.service"}, Success: true},
	})
	if !reflect.DeepEqual(client.statusRequests, []privileged.ServiceStatusRequest{
		{Units: []string{"veil-hysteria2@new.service"}},
		{Units: []string{"veil-hysteria2@new.service"}},
	}) {
		t.Fatalf("status requests=%+v", client.statusRequests)
	}
	if len(results) != 1 || results[0].Name != "veil-hysteria2@new.service" || !results[0].Healthy {
		t.Fatalf("health results=%+v", results)
	}
}

func TestPrivilegedApplyHealthChecksRequireStableOlcrtc(t *testing.T) {
	stubServiceHealthTiming(t, time.Millisecond, time.Second, 0)
	client := &recordingPrivilegedClient{statusActiveState: "active"}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	results := NewManagementApplyContext(state).checkServiceHealth([]ServiceActionResult{{
		Name: "veil-olcrtc@o1.service", Command: []string{"systemctl", "restart", "veil-olcrtc@o1.service"}, Success: true,
	}})
	if len(client.statusRequests) != 2 {
		t.Fatalf("olcrtc must not be treated healthy on the first active poll, requests=%d results=%+v", len(client.statusRequests), results)
	}
	if len(results) != 1 || !results[0].Healthy {
		t.Fatalf("health results=%+v", results)
	}
}

func TestPrivilegedApplyHealthChecksRejectOlcrtcCrashLoop(t *testing.T) {
	stubServiceHealthTiming(t, time.Millisecond, 40*time.Millisecond, 20*time.Millisecond)
	client := &recordingPrivilegedClient{statusActiveStates: []string{"active", "failed"}}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	results := NewManagementApplyContext(state).checkServiceHealth([]ServiceActionResult{{
		Name: "veil-olcrtc@o1.service", Command: []string{"systemctl", "restart", "veil-olcrtc@o1.service"}, Success: true,
	}})
	if len(results) != 1 || results[0].Healthy {
		t.Fatalf("crash-looping olcrtc must fail apply health, results=%+v requests=%d", results, len(client.statusRequests))
	}
}

func TestPrivilegedApplyHealthChecksTimeoutUnstableOlcrtc(t *testing.T) {
	stubServiceHealthTiming(t, time.Millisecond, 15*time.Millisecond, time.Second)
	client := &recordingPrivilegedClient{statusActiveState: "active"}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	results := NewManagementApplyContext(state).checkServiceHealth([]ServiceActionResult{{
		Name: "veil-olcrtc@o1.service", Command: []string{"systemctl", "restart", "veil-olcrtc@o1.service"}, Success: true,
	}})
	if len(results) != 1 || results[0].Healthy || !strings.Contains(results[0].Error, "did not stay active") {
		t.Fatalf("briefly-active olcrtc at timeout must be unhealthy, results=%+v", results)
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
	if len(c.statusActiveStates) > 0 {
		index := len(c.statusRequests) - 1
		if index >= len(c.statusActiveStates) {
			index = len(c.statusActiveStates) - 1
		}
		activeState = c.statusActiveStates[index]
	}
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
	lines := c.journalLines
	if lines == nil {
		lines = []string{"line one", "line two"}
	}
	return privileged.JournalResult{Unit: request.Unit, Lines: lines}, c.err
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
		return privileged.BackupResult{
			ArchiveName: request.ArchiveName,
			Archives:    []privileged.BackupArchive{{Name: request.ArchiveName, Size: 8, CreatedAt: "2026-06-05T12:00:00Z"}},
			Data:        []byte("VEILBACK"), TransactionID: "0123456789abcdef0123456789abcdef",
			ContentDigest: "5269965b9479151ca2cf88176e9e71af426fc170ed5a85f6940f186b26e8409d", InodeGeneration: "1:1:1", BoundSize: 8,
		}, nil
	case privileged.BackupActionVerify:
		return privileged.BackupResult{ArchiveName: request.ArchiveName, Verified: true}, nil
	case privileged.BackupActionPrune:
		return privileged.BackupResult{Pruned: []string{"old.enc"}, Kept: []string{"new.enc"}}, nil
	case privileged.BackupActionRestore:
		return privileged.BackupResult{
			ArchiveName: request.ArchiveName, Verified: true, Restored: true,
			SafetyStatePath: "state.safety", SafetyKeyPath: "key.safety",
		}, nil
	case privileged.BackupActionDelete:
		return privileged.BackupResult{ArchiveName: request.ArchiveName, Pruned: []string{request.ArchiveName}}, nil
	default:
		return privileged.BackupResult{}, errors.New("unexpected backup action")
	}
}

func (c *recordingPrivilegedClient) RotateKey(context.Context, privileged.RotateKeyRequest) error {
	c.rotateCalls++
	return c.err
}

func (c *recordingPrivilegedClient) RecoverKeyRotation(context.Context, privileged.RecoverKeyRotationRequest) error {
	c.recoverRotationCalls++
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
	if c.restartErr != nil {
		return c.restartErr
	}
	return c.err
}

func (c *recordingPrivilegedClient) SyncCaddyCert(_ context.Context, request privileged.SyncCaddyCertRequest) (privileged.SyncCaddyCertResult, error) {
	c.syncCaddyCertRequests = append(c.syncCaddyCertRequests, request)
	return privileged.SyncCaddyCertResult{Found: true, CertPath: "/etc/veil/certs/test.crt", KeyPath: "/etc/veil/certs/test.key"}, c.err
}

func TestPrivilegedServiceStatusAndLogsUseManagedUnits(t *testing.T) {
	client := &recordingPrivilegedClient{}
	router, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", Privileged: client})

	restart := httptest.NewRequest(http.MethodPost, "/api/services/caddy/restart", strings.NewReader(`{"confirm":true}`))
	restart.Header.Set("Content-Type", "application/json")
	restartResponse := httptest.NewRecorder()
	router.ServeHTTP(restartResponse, restart)
	if restartResponse.Code != http.StatusOK {
		t.Fatalf("restart status=%d body=%s", restartResponse.Code, restartResponse.Body.String())
	}
	wantAction := privileged.ServiceActionRequest{
		Unit: "veil-caddy.service", Action: privileged.ServiceActionRestart,
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
	router.ServeHTTP(logResponse, httptest.NewRequest(http.MethodGet, "/api/logs?unit=caddy&lines=500", nil))
	if logResponse.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", logResponse.Code, logResponse.Body.String())
	}
	if !reflect.DeepEqual(client.journals, []privileged.JournalRequest{{
		Unit: "veil-caddy.service", Lines: 500,
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
	_, state := newApplyTrackedRouterWithState(t)
	state.privileged = client
	state.updateStager = func(context.Context) (string, error) { return "v0.6.0", nil }
	routes := PanelRoutes{Info: ServerInfo{Version: "0.6.0"}, State: state}
	response := httptest.NewRecorder()
	routes.handleUpdateVersion(response, httptest.NewRequest(http.MethodPost, "/api/version/update", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	if len(client.updates) != 1 || client.updates[0].ArtifactID != "veil-update" || client.updates[0].Version != "v0.6.0" ||
		client.updates[0].Fence.Generation == 0 || client.updates[0].Fence.OperationID == "" {
		t.Fatalf("updates=%+v", client.updates)
	}
	deadline := time.Now().Add(3 * time.Second)
	for client.restartCalls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if client.restartCalls.Load() != 1 {
		t.Fatalf("restart calls=%d", client.restartCalls.Load())
	}
	var accepted struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil || accepted.JobID == "" {
		t.Fatalf("durable update response=%s err=%v", response.Body.String(), err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		persisted, err := state.getPanelUpdateJob(accepted.JobID)
		if err == nil && persisted.Status == "restarting" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("durable update job did not enter restarting state")
}

func TestPanelUpdateRestartFailureIsDurablyReported(t *testing.T) {
	client := &recordingPrivilegedClient{restartErr: errors.New("restart unavailable")}
	_, state := newApplyTrackedRouterWithState(t)
	state.privileged = client
	state.updateStager = func(context.Context) (string, error) { return "v0.6.0", nil }
	routes := PanelRoutes{Info: ServerInfo{Version: "0.5.0"}, State: state}
	response := httptest.NewRecorder()
	routes.handleUpdateVersion(response, httptest.NewRequest(http.MethodPost, "/api/version/update", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil || accepted.JobID == "" {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := state.getPanelUpdateJob(accepted.JobID)
		if err == nil && job.Status == "failed" {
			if !strings.Contains(job.Error, "restart unavailable") {
				t.Fatalf("job error=%q", job.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("restart failure was not persisted")
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
	router, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", Privileged: client})
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
	router, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", Privileged: client})
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
