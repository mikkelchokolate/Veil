package installer

import "time"

type BinaryDownloadRetryPolicy struct{}

func NewBinaryDownloadRetryPolicy() BinaryDownloadRetryPolicy { return BinaryDownloadRetryPolicy{} }

func (BinaryDownloadRetryPolicy) MaxAttempts() int { return 3 }

func (BinaryDownloadRetryPolicy) Backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	return time.Duration(1<<(attempt-2)) * time.Second
}

func (BinaryDownloadRetryPolicy) Retryable(err error) bool {
	if err == nil {
		return false
	}
	type temporary interface {
		Temporary() bool
	}
	if t, ok := err.(temporary); ok && t.Temporary() {
		return true
	}
	type timeout interface {
		Timeout() bool
	}
	if t, ok := err.(timeout); ok && t.Timeout() {
		return true
	}
	return false
}

// isRetryableNetError returns true for network errors that are worth retrying.
func isRetryableNetError(err error) bool {
	return NewBinaryDownloadRetryPolicy().Retryable(err)
}
