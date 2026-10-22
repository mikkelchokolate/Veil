package runtimeinstall

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

const runtimeHTTPAttempts = 3

// Retry only transport failures and temporary HTTP responses, never integrity checks.
// shortcut: metadata body reads are not retried; share whole-response retries if truncation becomes recurrent.
func runtimeHTTPDo(client *http.Client, request *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		response, err := client.Do(request.Clone(request.Context()))
		if !runtimeHTTPRetryable(response, err) || attempt+1 == runtimeHTTPAttempts || request.Context().Err() != nil {
			return response, err
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			_ = response.Body.Close()
		}
		if err := runtimeHTTPBackoff(request.Context(), attempt); err != nil {
			return nil, err
		}
	}
}

func runtimeHTTPRetryable(response *http.Response, err error) bool {
	if response != nil && response.StatusCode >= 400 {
		return response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	}
	var networkError net.Error
	return (errors.As(err, &networkError) && networkError.Timeout()) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

func runtimeHTTPBackoff(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
