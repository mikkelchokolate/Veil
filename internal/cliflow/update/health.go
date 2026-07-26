package update

import (
	"context"
	"fmt"
	"net/http"
	"time"

	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
)

func WaitForHealthy(addr string, token string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	candidates := statusflow.CandidateAddrs(addr)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		for _, candidate := range candidates {
			remaining = time.Until(deadline)
			if remaining <= 0 {
				break
			}
			requestTimeout := 2 * time.Second
			if remaining < requestTimeout {
				requestTimeout = remaining
			}
			url := candidate + "/healthz"
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				cancel()
				continue
			}
			if token != "" {
				req.Header.Set("X-Veil-Token", token)
			}
			resp, err := statusflow.HTTPClient(url).Do(req)
			cancel()
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
		remaining = time.Until(deadline)
		if remaining <= 0 {
			break
		}
		retryDelay := 100 * time.Millisecond
		if remaining < retryDelay {
			retryDelay = remaining
		}
		timer := time.NewTimer(retryDelay)
		<-timer.C
	}
	return fmt.Errorf("health check timed out after %v", timeout)
}
