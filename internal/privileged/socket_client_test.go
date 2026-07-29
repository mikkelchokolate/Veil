package privileged

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestSocketClientUsesOperationSpecificTimeouts(t *testing.T) {
	responseAfter := func(delay time.Duration, result string) string {
		return socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
			time.Sleep(delay)
			return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: []byte(result)}
		})
	}

	shortClient := NewSocketClient(responseAfter(30*time.Millisecond, `{"services":[]}`))
	shortClient.timeout = 10 * time.Millisecond
	if _, err := shortClient.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"veil.service"}}); err == nil {
		t.Fatal("short status operation unexpectedly exceeded its client deadline")
	}

	longClient := NewSocketClient(responseAfter(30*time.Millisecond, `{}`))
	longClient.timeout = 10 * time.Millisecond
	if _, err := longClient.Backup(context.Background(), BackupRequest{Action: BackupActionRestore, ArchiveName: "backup.enc"}); err != nil {
		t.Fatalf("restore inherited short helper deadline: %v", err)
	}

	boundedClient := NewSocketClient(responseAfter(30*time.Millisecond, `{}`))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := boundedClient.Backup(ctx, BackupRequest{Action: BackupActionRestore, ArchiveName: "backup.enc"}); err == nil {
		t.Fatal("restore ignored earlier request context deadline")
	}
}

func TestSocketClientCallsAllOperations(t *testing.T) {
	handler := func(request *RequestEnvelope) ResponseEnvelope {
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: []byte(`{"services":[]}`)}
	}
	path := socketTestServer(t, handler)
	client := NewSocketClient(path)
	ctx := context.Background()

	if _, err := client.Promote(ctx, PromoteRequest{ArtifactIDs: []string{"mieru"}}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if err := client.ServiceAction(ctx, ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart}); err != nil {
		t.Fatalf("service action: %v", err)
	}
	if _, err := client.ServiceStatus(ctx, ServiceStatusRequest{Units: []string{"veil.service"}}); err != nil {
		t.Fatalf("service status: %v", err)
	}
	if _, err := client.Journal(ctx, JournalRequest{Unit: "veil.service", Lines: 10}); err != nil {
		t.Fatalf("journal: %v", err)
	}
	if _, err := client.Backup(ctx, BackupRequest{Action: BackupActionList}); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := client.RotateKey(ctx, RotateKeyRequest{}); err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if err := client.RecoverKeyRotation(ctx, RecoverKeyRotationRequest{}); err != nil {
		t.Fatalf("recover key rotation: %v", err)
	}
	if _, err := client.FirewallApply(ctx, FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}}); err != nil {
		t.Fatalf("firewall apply: %v", err)
	}
	if _, err := client.StageUpdate(ctx, UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "v0.6.0"}); err != nil {
		t.Fatalf("stage update: %v", err)
	}
	if err := client.RestartPanel(ctx); err != nil {
		t.Fatalf("restart panel: %v", err)
	}
	if _, err := client.SyncCaddyCert(ctx, SyncCaddyCertRequest{Domain: "example.com"}); err != nil {
		t.Fatalf("sync caddy cert: %v", err)
	}
}

func TestSocketClientReturnsResults(t *testing.T) {
	handler := func(request *RequestEnvelope) ResponseEnvelope {
		var result any
		switch request.Operation {
		case OperationPromote:
			result = PromoteResult{BackupID: "b"}
		case OperationServiceStatus:
			result = ServiceStatusResult{Services: []ServiceStatus{{Unit: "veil.service", ActiveState: "active"}}}
		case OperationJournal:
			result = JournalResult{Unit: "veil.service", Lines: []string{"line"}}
		case OperationBackupCreate:
			result = BackupResult{ArchiveName: "daily.enc"}
		case OperationFirewallApply:
			result = FirewallResult{AppliedRuleIDs: []string{"allow-mieru-tcp"}}
		case OperationStageUpdate:
			result = UpdateResult{ArtifactID: "veil-linux-amd64", Installed: true}
		case OperationSyncCaddyCert:
			result = SyncCaddyCertResult{Found: true, CertPath: "/tmp/cert.crt"}
		default:
			result = struct{}{}
		}
		raw, _ := json.Marshal(result)
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: raw}
	}
	path := socketTestServer(t, handler)
	client := NewSocketClient(path)
	ctx := context.Background()

	promote, err := client.Promote(ctx, PromoteRequest{})
	if err != nil || promote.BackupID != "b" {
		t.Fatalf("promote result: %+v err=%v", promote, err)
	}
	status, err := client.ServiceStatus(ctx, ServiceStatusRequest{Units: []string{"veil.service"}})
	if err != nil || len(status.Services) != 1 {
		t.Fatalf("status result: %+v err=%v", status, err)
	}
	journal, err := client.Journal(ctx, JournalRequest{Unit: "veil.service", Lines: 10})
	if err != nil || len(journal.Lines) != 1 {
		t.Fatalf("journal result: %+v err=%v", journal, err)
	}
	backup, err := client.Backup(ctx, BackupRequest{Action: BackupActionCreate})
	if err != nil || backup.ArchiveName != "daily.enc" {
		t.Fatalf("backup result: %+v err=%v", backup, err)
	}
	firewall, err := client.FirewallApply(ctx, FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}})
	if err != nil || len(firewall.AppliedRuleIDs) != 1 {
		t.Fatalf("firewall result: %+v err=%v", firewall, err)
	}
	update, err := client.StageUpdate(ctx, UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "v0.6.0"})
	if err != nil || !update.Installed {
		t.Fatalf("update result: %+v err=%v", update, err)
	}
	cert, err := client.SyncCaddyCert(ctx, SyncCaddyCertRequest{Domain: "example.com"})
	if err != nil || !cert.Found {
		t.Fatalf("cert result: %+v err=%v", cert, err)
	}
}

func TestSocketClientDetectsResponseCorrelationMismatch(t *testing.T) {
	path := socketTestServer(t, func(_ *RequestEnvelope) ResponseEnvelope {
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: "wrong", OK: true}
	})
	client := NewSocketClient(path)
	_, err := client.Promote(context.Background(), PromoteRequest{})
	var opErr *Error
	if !errors.As(err, &opErr) || opErr.Code != ErrorOperationFailed {
		t.Fatalf("expected operation failed error, got %v", err)
	}
}

func TestSocketClientReturnsServerError(t *testing.T) {
	path := socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: false, Error: &Error{Code: ErrorForbiddenOperation, Message: "no"}}
	})
	client := NewSocketClient(path)
	err := client.ServiceAction(context.Background(), ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)
}

func TestSocketClientRejectsResultUnmarshalError(t *testing.T) {
	path := socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: []byte(`{not json`)}
	})
	client := NewSocketClient(path)
	_, err := client.Promote(context.Background(), PromoteRequest{})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSocketClientDialFailure(t *testing.T) {
	client := NewSocketClient(filepath.Join(t.TempDir(), "missing.sock"))
	err := client.RestartPanel(context.Background())
	if err == nil {
		t.Fatal("expected dial failure")
	}
}

func TestOperationForBackupActionMapsAllActions(t *testing.T) {
	cases := map[BackupAction]Operation{
		BackupActionCreate:  OperationBackupCreate,
		BackupActionList:    OperationBackupList,
		BackupActionVerify:  OperationBackupVerify,
		BackupActionRead:    OperationBackupRead,
		BackupActionPrune:   OperationBackupPrune,
		BackupActionRestore: OperationBackupRestore,
	}
	for action, want := range cases {
		got, err := operationForBackupAction(action)
		if err != nil {
			t.Fatalf("action %q: %v", action, err)
		}
		if got != want {
			t.Fatalf("action %q: got %q, want %q", action, got, want)
		}
	}
	_, err := operationForBackupAction(BackupAction("unknown"))
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func socketTestServer(t *testing.T, handler func(*RequestEnvelope) ResponseEnvelope) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var request RequestEnvelope
				if err := json.NewDecoder(bufio.NewReader(c)).Decode(&request); err != nil {
					return
				}
				response := handler(&request)
				_ = json.NewEncoder(c).Encode(response)
			}(conn)
		}
	}()
	return path
}

func TestSocketClientBackupRejectsUnsupportedAction(t *testing.T) {
	path := socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true}
	})
	client := NewSocketClient(path)
	_, err := client.Backup(context.Background(), BackupRequest{Action: BackupAction("")})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}
