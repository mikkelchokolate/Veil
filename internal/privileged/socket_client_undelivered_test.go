package privileged

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// A dial-phase failure proves the request never reached the helper, so the
// caller may treat the operation as provably not started. Transport errors
// after a successful dial must NOT be tagged undelivered: the helper may have
// received the request.
func TestSocketClientDialFailureIsUndelivered(t *testing.T) {
	client := NewSocketClient(filepath.Join(t.TempDir(), "missing.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.ServiceAction(ctx, ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart})
	if err == nil {
		t.Fatal("operation against a missing socket must fail")
	}
	if !IsUndelivered(err) {
		t.Fatalf("dial failure must be tagged undelivered, got %T %v", err, err)
	}
	// The error must still read as an operation failure for reporting.
	var operationError *Error
	if !errors.As(err, &operationError) || operationError.Code != ErrorOperationFailed {
		t.Fatalf("undelivered error must preserve the operation_failed surface, got %#v", err)
	}
}

func TestSocketClientUndeliveredSurvivesWrapping(t *testing.T) {
	client := NewSocketClient(filepath.Join(t.TempDir(), "missing.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, promoteErr := client.Promote(ctx, PromoteRequest{ArtifactIDs: []string{"rules/geoip.dat"}})
	if promoteErr == nil {
		t.Fatal("promote against a missing socket must fail")
	}
	wrapped := fmt.Errorf("promote staged configs: %w", promoteErr)
	if !IsUndelivered(wrapped) {
		t.Fatalf("wrapped dial failure must stay undelivered, got %T %v", wrapped, wrapped)
	}
}

func TestIsUndeliveredRejectsNonDialErrors(t *testing.T) {
	if IsUndelivered(nil) {
		t.Fatal("nil error is not undelivered")
	}
	if IsUndelivered(errors.New("helper rejected the request")) {
		t.Fatal("plain errors must not be tagged undelivered")
	}
	if IsUndelivered(newError(ErrorOperationFailed, "helper operation failed")) {
		t.Fatal("helper-side operation failures must not be tagged undelivered")
	}
}
