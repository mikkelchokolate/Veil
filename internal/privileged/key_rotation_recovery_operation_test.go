package privileged

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestRecoverKeyRotationRequestValidatesAndMatchesOperation(t *testing.T) {
	request := RequestEnvelope{
		Version:            ProtocolVersion,
		RequestID:          "recover-key-rotation",
		Operation:          OperationRecoverKeyRotation,
		RecoverKeyRotation: &RecoverKeyRotationRequest{},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate recover_key_rotation request: %v", err)
	}
	if !request.payloadMatchesOperation() {
		t.Fatal("recover_key_rotation payload did not match operation")
	}
}

func TestRecoverKeyRotationDispatchesThroughAdapterAndServer(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RecoverKeyRotation: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	response := servePipeRequest(t, server, RequestEnvelope{
		Version:            ProtocolVersion,
		RequestID:          "recover-key-rotation",
		Operation:          OperationRecoverKeyRotation,
		RecoverKeyRotation: &RecoverKeyRotationRequest{},
	})
	if !response.OK || response.Error != nil {
		t.Fatalf("recover response: %+v", response)
	}
	if calls.Load() != 1 {
		t.Fatalf("recovery executor calls=%d, want 1", calls.Load())
	}
}

func TestSocketClientRecoverKeyRotationUsesDedicatedOperation(t *testing.T) {
	seen := make(chan RequestEnvelope, 1)
	path := socketTestServer(t, func(request *RequestEnvelope) ResponseEnvelope {
		seen <- *request
		return ResponseEnvelope{Version: ProtocolVersion, RequestID: request.RequestID, OK: true}
	})
	if err := NewSocketClient(path).RecoverKeyRotation(context.Background(), RecoverKeyRotationRequest{}); err != nil {
		t.Fatalf("recover through socket: %v", err)
	}
	request := <-seen
	if request.Operation != OperationRecoverKeyRotation || request.RecoverKeyRotation == nil {
		t.Fatalf("unexpected recovery request: %+v", request)
	}
	if request.RotateKey != nil {
		t.Fatal("recovery request reused rotate_key payload")
	}
}

func TestProductionRecoveryNoJournalIsSuccessfulNoOp(t *testing.T) {
	root := t.TempDir()
	policy := testPolicy(t)
	policy.StatePath = root + "/state.json"
	policy.KeyPath = root + "/state.key"
	executor := NewProductionExecutor(DefaultProductionConfig(policy, "test"))
	adapter := NewLocalAdapter(policy, executor)
	if err := adapter.RecoverKeyRotation(context.Background(), RecoverKeyRotationRequest{}); err != nil {
		t.Fatalf("no-journal recovery: %v", err)
	}
}
