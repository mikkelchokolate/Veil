package privileged

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const maxRequestBytes int64 = 1 << 20

var ErrUnixPeerCredentialsUnsupported = errors.New("Unix peer credential verification is unsupported on this platform")

type Server struct {
	client  Client
	timeout time.Duration
}

func NewServer(client Client) *Server {
	return &Server{client: client, timeout: 30 * time.Second}
}

func (s *Server) ServeConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	s.setDeadline(ctx, conn)

	request, err := decodeRequest(conn)
	if err != nil {
		s.writeResponse(conn, ResponseEnvelope{
			Version: ProtocolVersion,
			Error:   newError(ErrorInvalidRequest, err.Error()),
		})
		return
	}
	if err := request.Validate(); err != nil {
		s.writeResponse(conn, ResponseEnvelope{
			Version:   ProtocolVersion,
			RequestID: request.RequestID,
			Error:     newError(ErrorInvalidRequest, err.Error()),
		})
		return
	}
	result, err := s.dispatch(ctx, request)
	if err != nil {
		var operationError *Error
		if !errors.As(err, &operationError) {
			operationError = newError(ErrorOperationFailed, err.Error())
		}
		s.writeResponse(conn, ResponseEnvelope{
			Version:   ProtocolVersion,
			RequestID: request.RequestID,
			Error:     operationError,
		})
		return
	}
	rawResult, err := json.Marshal(result)
	if err != nil {
		s.writeResponse(conn, ResponseEnvelope{
			Version:   ProtocolVersion,
			RequestID: request.RequestID,
			Error:     newError(ErrorOperationFailed, "encode operation result"),
		})
		return
	}
	s.writeResponse(conn, ResponseEnvelope{
		Version:   ProtocolVersion,
		RequestID: request.RequestID,
		OK:        true,
		Result:    rawResult,
	})
}

func (s *Server) setDeadline(ctx context.Context, conn net.Conn) {
	deadline := time.Now().Add(s.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
}

func decodeRequest(reader io.Reader) (RequestEnvelope, error) {
	limited := &io.LimitedReader{R: reader, N: maxRequestBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var request RequestEnvelope
	if err := decoder.Decode(&request); err != nil {
		if limited.N == 0 {
			return RequestEnvelope{}, errors.New("request exceeds 1 MiB limit")
		}
		return RequestEnvelope{}, err
	}
	if limited.N == 0 {
		return RequestEnvelope{}, errors.New("request exceeds 1 MiB limit")
	}
	buffered, err := io.ReadAll(decoder.Buffered())
	if err != nil {
		return RequestEnvelope{}, err
	}
	if len(bytes.TrimSpace(buffered)) != 0 {
		return RequestEnvelope{}, errors.New("only one JSON request is allowed per connection")
	}
	return request, nil
}

func (s *Server) dispatch(ctx context.Context, request RequestEnvelope) (any, error) {
	if s.client == nil {
		return nil, newError(ErrorOperationFailed, "privileged client is unavailable")
	}
	switch request.Operation {
	case OperationPromote:
		return s.client.Promote(ctx, *request.Promote)
	case OperationServiceAction:
		if err := s.client.ServiceAction(ctx, *request.ServiceAction); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	case OperationServiceStatus:
		return s.client.ServiceStatus(ctx, *request.ServiceStatus)
	case OperationJournal:
		return s.client.Journal(ctx, *request.Journal)
	case OperationBackupCreate, OperationBackupList, OperationBackupVerify, OperationBackupPrune, OperationBackupRestore:
		expected := backupActionForOperation(request.Operation)
		if request.Backup.Action != expected {
			return nil, newError(ErrorInvalidRequest, "backup action does not match operation")
		}
		return s.client.Backup(ctx, *request.Backup)
	case OperationRotateKey:
		if err := s.client.RotateKey(ctx, *request.RotateKey); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	case OperationFirewallApply:
		return s.client.FirewallApply(ctx, *request.Firewall)
	case OperationStageUpdate:
		return s.client.StageUpdate(ctx, *request.Update)
	case OperationRestartPanel:
		if err := s.client.RestartPanel(ctx); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	default:
		return nil, newError(ErrorInvalidRequest, "unsupported operation")
	}
}

func backupActionForOperation(operation Operation) BackupAction {
	switch operation {
	case OperationBackupCreate:
		return BackupActionCreate
	case OperationBackupList:
		return BackupActionList
	case OperationBackupVerify:
		return BackupActionVerify
	case OperationBackupPrune:
		return BackupActionPrune
	case OperationBackupRestore:
		return BackupActionRestore
	default:
		return ""
	}
}

func (s *Server) writeResponse(writer io.Writer, response ResponseEnvelope) {
	_ = json.NewEncoder(writer).Encode(response)
}

func validateSocketPath(path string) error {
	if !filepath.IsAbs(path) {
		return newError(ErrorInvalidRequest, "helper socket path must be absolute")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return newError(ErrorForbiddenOperation, "helper socket path must not be a symlink")
	}
	if info.Mode()&os.ModeSocket == 0 {
		return newError(ErrorConflict, "helper socket path exists and is not a socket")
	}
	return nil
}
