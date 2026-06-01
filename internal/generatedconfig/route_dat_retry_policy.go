package generatedconfig

import "time"

type RouteDatRetryPolicy struct{}

func NewRouteDatRetryPolicy() RouteDatRetryPolicy { return RouteDatRetryPolicy{} }

func (RouteDatRetryPolicy) MaxAttempts() int { return 3 }

func (RouteDatRetryPolicy) Backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	return time.Duration(1<<(attempt-2)) * time.Second
}

func (RouteDatRetryPolicy) Retryable(err error) bool {
	if err == nil {
		return false
	}
	type temporary interface{ Temporary() bool }
	if t, ok := err.(temporary); ok && t.Temporary() {
		return true
	}
	type timeout interface{ Timeout() bool }
	if t, ok := err.(timeout); ok && t.Timeout() {
		return true
	}
	return false
}
