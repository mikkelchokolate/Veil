package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

func WaitForHealthy(addr string, token string, timeout time.Duration) error {
	return WaitForHealthyAt(addr, token, statusflow.ResolveWebBasePath(""), timeout)
}

func WaitForHealthyAt(addr, token, webBasePath string, timeout time.Duration) error {
	basePath, err := webbasepath.Normalize(webBasePath)
	if err != nil {
		return fmt.Errorf("invalid web base path: %w", err)
	}
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
			url := strings.TrimRight(candidate, "/") + basePath + "healthz"
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
				// Require the Veil health contract ({"status":"ok"}): any
				// other 200 — e.g. a foreign service or a stale listener on
				// the same port — must not be treated as the restarted panel.
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
				resp.Body.Close()
				if statusflow.HealthzStatusOK(body) {
					return nil
				}
				continue
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
