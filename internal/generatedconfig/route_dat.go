package generatedconfig

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const maxRouteDatSize = 50 * 1024 * 1024 // 50 MB

var routeDatHTTPClient = &http.Client{Timeout: 30 * time.Second}
var routeDatDownloader = DownloadRouteDat

func DownloadRouteDat(url string) ([]byte, error) {
	retry := NewRouteDatRetryPolicy()
	maxAttempts := retry.MaxAttempts()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := retry.Backoff(attempt)
			log.Printf("downloadRouteDat: retry attempt %d/%d after %v (previous error: %v)", attempt, maxAttempts, backoff, lastErr)
			time.Sleep(backoff)
		}
		resp, err := routeDatHTTPClient.Get(url)
		if err != nil {
			lastErr = err
			if !retry.Retryable(err) {
				return nil, err
			}
			continue
		}
		decision := NewRouteDatResponsePolicy().Decide(url, resp.StatusCode, resp.Status)
		if decision.Retry {
			resp.Body.Close()
			lastErr = decision.Err
			continue
		}
		if !decision.Accept {
			resp.Body.Close()
			return nil, decision.Err
		}
		defer resp.Body.Close()
		limit := NewRouteDatBodyLimit(maxRouteDatSize)
		lr := io.LimitReader(resp.Body, int64(limit.Limit())+1)
		body, err := io.ReadAll(lr)
		if err != nil {
			lastErr = err
			if !retry.Retryable(err) {
				return nil, err
			}
			continue
		}
		if err := limit.Validate(url, body); err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, fmt.Errorf("download %s failed after %d attempts: %w", url, maxAttempts, lastErr)
}

func fetchVerifiedRouteDatFile(file RoutingSourceFile) ([]byte, error) {
	return NewRoutingSourceMaterial("", RoutingSource{}).Fetch(file)
}

func downloadRouteDat(url string) ([]byte, error) {
	return DownloadRouteDat(url)
}
