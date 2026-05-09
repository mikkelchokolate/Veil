package update

import (
	"context"
	"fmt"
	"net/http"
	"time"

	statusflow "github.com/veil-panel/veil/internal/cliflow/status"
)

func WaitForHealthy(addr string, token string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	candidates := statusflow.CandidateAddrs(addr)
	for time.Now().Before(deadline) {
		for _, candidate := range candidates {
			url := candidate + "/healthz"
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out after %v", timeout)
}
