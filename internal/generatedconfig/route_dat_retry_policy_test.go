package generatedconfig

import (
	"errors"
	"testing"
	"time"
)

func TestRouteDatRetryPolicyDefinesAttemptsBackoffAndRetryableErrors(t *testing.T) {
	policy := NewRouteDatRetryPolicy()
	if policy.MaxAttempts() != 3 {
		t.Fatalf("max attempts = %d", policy.MaxAttempts())
	}
	if policy.Backoff(1) != 0 || policy.Backoff(2) != time.Second || policy.Backoff(3) != 2*time.Second {
		t.Fatalf("unexpected backoff sequence")
	}
	if !policy.Retryable(routeDatTemporaryError{}) {
		t.Fatal("temporary error should be retryable")
	}
	if policy.Retryable(errors.New("plain")) {
		t.Fatal("plain error should not be retryable")
	}
}

type routeDatTemporaryError struct{}

func (routeDatTemporaryError) Error() string   { return "temporary" }
func (routeDatTemporaryError) Temporary() bool { return true }
