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
		lr := io.LimitReader(resp.Body, maxRouteDatSize+1)
		body, err := io.ReadAll(lr)
		if err != nil {
			lastErr = err
			if !retry.Retryable(err) {
				return nil, err
			}
			continue
		}
		if len(body) > maxRouteDatSize {
			return nil, fmt.Errorf("download %s exceeds maximum size of %d bytes", url, maxRouteDatSize)
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
	fields := strings.Fields(checksumText)
	if len(fields) == 0 {
		return fmt.Errorf("checksum for %s is empty", name)
	}
	expected := ""
	for i := 0; i < len(fields); i++ {
		if fields[i] == name && i > 0 {
			expected = fields[i-1]
			break
		}
	}
	if expected == "" {
		expected = fields[0]
	}
	expected = strings.TrimPrefix(strings.ToLower(expected), "sha256:")
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid checksum for %s", name)
	}
	actual := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}
