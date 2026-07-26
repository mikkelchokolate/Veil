package privileged

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerHandlesOneJSONRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		ServiceAction: func(_ context.Context, request ServiceActionRequest) error {
			calls.Add(1)
			if request.Unit != "veil.service" || request.Action != ServiceActionRestart {
				t.Fatalf("unexpected service action: %+v", request)
			}
			return nil
		},
	}))
	request := RequestEnvelope{
		Version:       ProtocolVersion,
		RequestID:     "round-trip",
		Operation:     OperationServiceAction,
		ServiceAction: &ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart},
	}
	response := servePipeRequest(t, server, request)
	if !response.OK || response.Error != nil {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.Version != ProtocolVersion || response.RequestID != request.RequestID {
		t.Fatalf("response correlation mismatch: %+v", response)
	}
	if calls.Load() != 1 {
		t.Fatalf("want one executor call, got %d", calls.Load())
	}
}

func TestServerRejectsProtocolMismatchAndUnknownFields(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	tests := map[string]string{
		"version": `{"version":99,"requestId":"bad-version","operation":"restart_panel","restartPanel":{}}`,
		"unknown": `{"version":1,"requestId":"unknown","operation":"restart_panel","restartPanel":{},"command":"reboot"}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			response := servePipeRaw(t, server, raw)
			if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
				t.Fatalf("expected invalid_request response, got %+v", response)
			}
		})
	}
}

func TestServerRejectsMultiplePayloadsBeforeExecution(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	raw := `{"version":1,"requestId":"multi","operation":"restart_panel","restartPanel":{},"journal":{"unit":"veil.service","lines":10}}`
	response := servePipeRaw(t, server, raw)
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request response, got %+v", response)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor called %d times", calls.Load())
	}
}

func TestServerRejectsRequestsLargerThanOneMiB(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	raw := `{"version":1,"requestId":"` + strings.Repeat("x", (1<<20)+1) + `","operation":"restart_panel","restartPanel":{}}`
	response := servePipeRaw(t, server, raw)
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request response, got %+v", response)
	}
}

func TestServerRejectsTwoRequestsOnOneConnection(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	raw := `{"version":1,"requestId":"first","operation":"restart_panel","restartPanel":{}}` +
		`{"version":1,"requestId":"second","operation":"restart_panel","restartPanel":{}}`
	response := servePipeRaw(t, server, raw)
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request response, got %+v", response)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor called before multi-request rejection: %d", calls.Load())
	}
}

func TestServerHonorsContextDeadline(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	client, helper := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		server.ServeConn(ctx, helper)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeConn ignored context deadline")
	}
}

func TestValidateSocketPathRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.sock")
	link := filepath.Join(root, "helper.sock")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := validateSocketPath(link); err == nil {
		t.Fatal("expected symlink socket path rejection")
	}
}

func servePipeRequest(t *testing.T, server *Server, request RequestEnvelope) ResponseEnvelope {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return servePipeRaw(t, server, string(raw))
}

func servePipeRaw(t *testing.T, server *Server, raw string) ResponseEnvelope {
	t.Helper()
	client, helper := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.ServeConn(context.Background(), helper)
		close(done)
	}()
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte(raw + "\n"))
		writeDone <- err
	}()
	var response ResponseEnvelope
	if err := json.NewDecoder(bufio.NewReader(client)).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	client.Close()
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("request writer did not finish")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not close after one response")
	}
	return response
}

func TestServerDispatchesAllOperations(t *testing.T) {
	policy := testPolicy(t)
	tests := []struct {
		name      string
		operation Operation
		payload   any
		setup     func(*Executor)
		wantOK    bool
	}{
		{
			name: "promote", operation: OperationPromote, payload: &PromoteRequest{ArtifactIDs: []string{"mieru"}},
			setup: func(e *Executor) {
				e.Promote = func(context.Context, ResolvedPromotion) (PromoteResult, error) { return PromoteResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "service_action", operation: OperationServiceAction, payload: &ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart},
			setup: func(e *Executor) { e.ServiceAction = func(context.Context, ServiceActionRequest) error { return nil } }, wantOK: true,
		},
		{
			name: "service_status", operation: OperationServiceStatus, payload: &ServiceStatusRequest{Units: []string{"veil.service"}},
			setup: func(e *Executor) {
				e.ServiceStatus = func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error) {
					return ServiceStatusResult{}, nil
				}
			}, wantOK: true,
		},
		{
			name: "journal", operation: OperationJournal, payload: &JournalRequest{Unit: "veil.service", Lines: 10},
			setup: func(e *Executor) {
				e.Journal = func(context.Context, ResolvedJournal) (JournalResult, error) { return JournalResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "backup_create", operation: OperationBackupCreate, payload: &BackupRequest{Action: BackupActionCreate},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "backup_list", operation: OperationBackupList, payload: &BackupRequest{Action: BackupActionList},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "backup_verify", operation: OperationBackupVerify, payload: &BackupRequest{Action: BackupActionVerify, ArchiveName: "daily.enc"},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "backup_read", operation: OperationBackupRead, payload: &BackupRequest{Action: BackupActionRead, ArchiveName: "daily.enc"},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) {
					return BackupResult{Data: []byte("x")}, nil
				}
			}, wantOK: true,
		},
		{
			name: "backup_prune", operation: OperationBackupPrune, payload: &BackupRequest{Action: BackupActionPrune},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "backup_restore", operation: OperationBackupRestore, payload: &BackupRequest{Action: BackupActionRestore, ArchiveName: "daily.enc"},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "rotate_key", operation: OperationRotateKey, payload: &RotateKeyRequest{},
			setup: func(e *Executor) { e.RotateKey = func(context.Context, RotateKeyRequest) error { return nil } }, wantOK: true,
		},
		{
			name: "recover_key_rotation", operation: OperationRecoverKeyRotation, payload: &RecoverKeyRotationRequest{},
			setup: func(e *Executor) { e.RecoverKeyRotation = func(context.Context) error { return nil } }, wantOK: true,
		},
		{
			name: "firewall_apply", operation: OperationFirewallApply, payload: &FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}},
			setup: func(e *Executor) {
				e.Firewall = func(context.Context, ResolvedFirewall) (FirewallResult, error) { return FirewallResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "stage_update", operation: OperationStageUpdate, payload: &UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "v0.6.0"},
			setup: func(e *Executor) {
				e.Update = func(context.Context, ResolvedUpdate) (UpdateResult, error) { return UpdateResult{}, nil }
			}, wantOK: true,
		},
		{
			name: "restart_panel", operation: OperationRestartPanel, payload: &RestartPanelRequest{},
			setup: func(e *Executor) { e.RestartPanel = func(context.Context) error { return nil } }, wantOK: true,
		},
		{
			name: "sync_caddy_cert", operation: OperationSyncCaddyCert, payload: &SyncCaddyCertRequest{Domain: "example.com"},
			setup: func(e *Executor) {
				e.SyncCaddyCert = func(context.Context, SyncCaddyCertRequest) (SyncCaddyCertResult, error) {
					return SyncCaddyCertResult{}, nil
				}
			}, wantOK: true,
		},
		{
			name: "backup_action_mismatch", operation: OperationBackupCreate, payload: &BackupRequest{Action: BackupActionList},
			setup: func(e *Executor) {
				e.Backup = func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, nil }
			}, wantOK: false,
		},
		{
			name: "unsupported_operation", operation: Operation("bad"), payload: &RestartPanelRequest{},
			setup: func(e *Executor) {}, wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executor := Executor{}
			tc.setup(&executor)
			server := NewServer(NewLocalAdapter(policy, executor))
			request := RequestEnvelope{Version: ProtocolVersion, RequestID: tc.name, Operation: tc.operation}
			switch tc.operation {
			case OperationPromote:
				request.Promote = tc.payload.(*PromoteRequest)
			case OperationServiceAction:
				request.ServiceAction = tc.payload.(*ServiceActionRequest)
			case OperationServiceStatus:
				request.ServiceStatus = tc.payload.(*ServiceStatusRequest)
			case OperationJournal:
				request.Journal = tc.payload.(*JournalRequest)
			case OperationBackupCreate, OperationBackupList, OperationBackupVerify, OperationBackupRead, OperationBackupPrune, OperationBackupRestore:
				request.Backup = tc.payload.(*BackupRequest)
			case OperationRotateKey:
				request.RotateKey = tc.payload.(*RotateKeyRequest)
			case OperationRecoverKeyRotation:
				request.RecoverKeyRotation = tc.payload.(*RecoverKeyRotationRequest)
			case OperationFirewallApply:
				request.Firewall = tc.payload.(*FirewallRequest)
			case OperationStageUpdate:
				request.Update = tc.payload.(*UpdateRequest)
			case OperationRestartPanel:
				request.RestartPanel = tc.payload.(*RestartPanelRequest)
			case OperationSyncCaddyCert:
				request.SyncCaddyCert = tc.payload.(*SyncCaddyCertRequest)
			default:
				request.RestartPanel = tc.payload.(*RestartPanelRequest)
			}
			response := servePipeRequest(t, server, request)
			if tc.wantOK && (!response.OK || response.Error != nil) {
				t.Fatalf("expected ok response, got %+v", response)
			}
			if !tc.wantOK && (response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest) {
				t.Fatalf("expected invalid_request error, got %+v", response)
			}
		})
	}
}

func TestServerDispatchReturnsErrorWhenClientNil(t *testing.T) {
	server := NewServer(nil)
	request := RequestEnvelope{Version: ProtocolVersion, RequestID: "nil-client", Operation: OperationRestartPanel, RestartPanel: &RestartPanelRequest{}}
	response := servePipeRequest(t, server, request)
	if response.OK || response.Error == nil || response.Error.Code != ErrorOperationFailed {
		t.Fatalf("expected operation_failed error, got %+v", response)
	}
}

func TestBackupActionForOperation(t *testing.T) {
	for op, want := range map[Operation]BackupAction{
		OperationBackupCreate:  BackupActionCreate,
		OperationBackupList:    BackupActionList,
		OperationBackupVerify:  BackupActionVerify,
		OperationBackupRead:    BackupActionRead,
		OperationBackupPrune:   BackupActionPrune,
		OperationBackupRestore: BackupActionRestore,
		OperationPromote:       "",
	} {
		if got := backupActionForOperation(op); got != want {
			t.Fatalf("backupActionForOperation(%q) = %q, want %q", op, got, want)
		}
	}
}

func TestValidateSocketPathAcceptsMissingAbsoluteAndRejectsNonSocket(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.sock")
	if err := validateSocketPath(missing); err != nil {
		t.Fatalf("missing absolute path should be valid: %v", err)
	}
	relative := "helper.sock"
	if err := validateSocketPath(relative); err == nil {
		t.Fatal("expected relative path rejection")
	}
	notSocket := filepath.Join(root, "file")
	if err := os.WriteFile(notSocket, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSocketPath(notSocket); err == nil {
		t.Fatal("expected non-socket rejection")
	}
}
