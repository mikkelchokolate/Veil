package runtimeinstall

import (
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Retry only transport failures and temporary HTTP responses, never integrity checks.
func runtimeHTTPDo(client *http.Client, request *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		response, err := client.Do(request.Clone(request.Context()))
		var networkError net.Error
		retry := (errors.As(err, &networkError) && networkError.Timeout()) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET)
		if response != nil {
			retry = response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		}
		if !retry || attempt == 2 || request.Context().Err() != nil {
			return response, err
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			_ = response.Body.Close()
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
		select {
		case <-request.Context().Done():
			timer.Stop()
			return nil, request.Context().Err()
		case <-timer.C:
		}
	}
}
