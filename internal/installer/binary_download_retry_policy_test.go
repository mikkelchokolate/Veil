package installer

import (
	"errors"
	"testing"
	"time"
)

func TestBinaryDownloadRetryPolicyDefinesAttemptsBackoffAndRetryableErrors(t *testing.T) {
	policy := NewBinaryDownloadRetryPolicy()
	if policy.MaxAttempts() != 3 {
		t.Fatalf("max attempts = %d", policy.MaxAttempts())
	}
	if policy.Backoff(1) != 0 || policy.Backoff(2) != time.Second || policy.Backoff(3) != 2*time.Second {
		t.Fatalf("unexpected backoff sequence")
	}
	if !policy.Retryable(temporaryRetryError{}) {
		t.Fatal("temporary error should be retryable")
	}
	if policy.Retryable(errors.New("plain")) {
		t.Fatal("plain error should not be retryable")
	}
}

type temporaryRetryError struct{}

func (temporaryRetryError) Error() string   { return "temporary" }
func (temporaryRetryError) Temporary() bool { return true }
