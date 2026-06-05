package privileged

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

type SocketClient struct {
	path    string
	timeout time.Duration
}

func NewSocketClient(path string) *SocketClient {
	return &SocketClient{path: path, timeout: 30 * time.Second}
}

func (c *SocketClient) Promote(ctx context.Context, request PromoteRequest) (PromoteResult, error) {
	var result PromoteResult
	err := c.call(ctx, RequestEnvelope{Operation: OperationPromote, Promote: &request}, &result)
	return result, err
}

func (c *SocketClient) ServiceAction(ctx context.Context, request ServiceActionRequest) error {
	return c.call(ctx, RequestEnvelope{Operation: OperationServiceAction, ServiceAction: &request}, nil)
}

func (c *SocketClient) ServiceStatus(ctx context.Context, request ServiceStatusRequest) (ServiceStatusResult, error) {
	var result ServiceStatusResult
	err := c.call(ctx, RequestEnvelope{Operation: OperationServiceStatus, ServiceStatus: &request}, &result)
	return result, err
}

func (c *SocketClient) Journal(ctx context.Context, request JournalRequest) (JournalResult, error) {
	var result JournalResult
	err := c.call(ctx, RequestEnvelope{Operation: OperationJournal, Journal: &request}, &result)
	return result, err
}

func (c *SocketClient) Backup(ctx context.Context, request BackupRequest) (BackupResult, error) {
	operation, err := operationForBackupAction(request.Action)
	if err != nil {
		return BackupResult{}, err
	}
	var result BackupResult
	err = c.call(ctx, RequestEnvelope{Operation: operation, Backup: &request}, &result)
	return result, err
}

func (c *SocketClient) RotateKey(ctx context.Context, request RotateKeyRequest) error {
	return c.call(ctx, RequestEnvelope{Operation: OperationRotateKey, RotateKey: &request}, nil)
}

func (c *SocketClient) FirewallApply(ctx context.Context, request FirewallRequest) (FirewallResult, error) {
	var result FirewallResult
	err := c.call(ctx, RequestEnvelope{Operation: OperationFirewallApply, Firewall: &request}, &result)
	return result, err
}

func (c *SocketClient) StageUpdate(ctx context.Context, request UpdateRequest) (UpdateResult, error) {
	var result UpdateResult
	err := c.call(ctx, RequestEnvelope{Operation: OperationStageUpdate, Update: &request}, &result)
	return result, err
}

func (c *SocketClient) RestartPanel(ctx context.Context) error {
	request := RestartPanelRequest{}
	return c.call(ctx, RequestEnvelope{Operation: OperationRestartPanel, RestartPanel: &request}, nil)
}

func (c *SocketClient) call(ctx context.Context, request RequestEnvelope, result any) error {
	request.Version = ProtocolVersion
	request.RequestID = newRequestID()
	if err := request.Validate(); err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.path)
	if err != nil {
		return wrapOperationError(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return wrapOperationError(err)
	}
	if unixConn, ok := conn.(*net.UnixConn); ok {
		_ = unixConn.CloseWrite()
	}
	decoder := json.NewDecoder(io.LimitReader(conn, maxResponseBytes+1))
	decoder.DisallowUnknownFields()
	var response ResponseEnvelope
	if err := decoder.Decode(&response); err != nil {
		return wrapOperationError(err)
	}
	if response.Version != ProtocolVersion || response.RequestID != request.RequestID {
		return newError(ErrorOperationFailed, "helper response correlation mismatch")
	}
	if !response.OK {
		if response.Error != nil {
			return response.Error
		}
		return newError(ErrorOperationFailed, "helper operation failed")
	}
	if result == nil || len(response.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return wrapOperationError(err)
	}
	return nil
}

func operationForBackupAction(action BackupAction) (Operation, error) {
	switch action {
	case BackupActionCreate:
		return OperationBackupCreate, nil
	case BackupActionList:
		return OperationBackupList, nil
	case BackupActionVerify:
		return OperationBackupVerify, nil
	case BackupActionRead:
		return OperationBackupRead, nil
	case BackupActionPrune:
		return OperationBackupPrune, nil
	case BackupActionRestore:
		return OperationBackupRestore, nil
	default:
		return "", newError(ErrorInvalidRequest, "unsupported backup action")
	}
}

const maxResponseBytes int64 = 96 * 1024 * 1024

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("request-%d", time.Now().UnixNano())
}

var _ Client = (*SocketClient)(nil)
