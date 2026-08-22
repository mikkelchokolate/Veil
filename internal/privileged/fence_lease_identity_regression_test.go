package privileged

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequiredFenceRejectsMissingOperationIdentity(t *testing.T) {
	guard := newFenceGuard("", true)
	var token FenceToken
	if err := json.Unmarshal([]byte(`{"owner":"owner-a","generation":1,"leaseExpiresAt":4102444800}`), &token); err != nil {
		t.Fatal(err)
	}
	if err := guard.Accept(token); err == nil {
		t.Fatal("required fence accepted a token without an operation ID")
	}
}

func TestRequiredFenceRejectsExpiredLease(t *testing.T) {
	guard := newFenceGuard("", true)
	var token FenceToken
	encoded := []byte(`{"owner":"owner-a","generation":1,"operationId":"apply-1","leaseExpiresAt":1}`)
	if err := json.Unmarshal(encoded, &token); err != nil {
		t.Fatal(err)
	}
	if err := guard.Accept(token); err == nil {
		t.Fatalf("required fence accepted a lease that expired before %s", time.Unix(1, 0).UTC())
	}
}

func TestFenceBindsGenerationToOneOperation(t *testing.T) {
	guard := newFenceGuard("", true)
	var first, replay FenceToken
	if err := json.Unmarshal([]byte(`{"owner":"owner-a","generation":7,"operationId":"apply-7","leaseExpiresAt":4102444800}`), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"owner":"owner-a","generation":7,"operationId":"different-operation","leaseExpiresAt":4102444800}`), &replay); err != nil {
		t.Fatal(err)
	}
	if err := guard.Accept(first); err != nil {
		t.Fatalf("valid first fence rejected: %v", err)
	}
	if err := guard.Accept(replay); err == nil {
		t.Fatal("same owner/generation was authorized for a different operation")
	}
}
