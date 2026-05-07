package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const maxRouteDatSize = 50 * 1024 * 1024 // 50 MB

var routeDatHTTPClient = &http.Client{Timeout: 30 * time.Second}
var routeDatDownloader = downloadRouteDat

func downloadRouteDat(url string) ([]byte, error) {
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
	body, err := routeDatDownloader(file.URL)
	if err != nil {
		return nil, err
	}
	if file.SHA256URL == "" {
		return body, nil
	}
	checksumBody, err := routeDatDownloader(file.SHA256URL)
	if err != nil {
		return nil, err
	}
	if err := verifyRouteDatChecksum(file.Name, body, string(checksumBody)); err != nil {
		return nil, err
	}
	return body, nil
}

func verifyRouteDatChecksum(name string, body []byte, checksumText string) error {
	expected, err := NewRouteDatChecksumParser().Parse(name, checksumText)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}
