package installer

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BinaryAcquisitionModule struct {
	binary BinaryAcquisition
}

func NewBinaryAcquisitionModule(binary BinaryAcquisition) BinaryAcquisitionModule {
	return BinaryAcquisitionModule{binary: binary}
}

func (m BinaryAcquisitionModule) RepairPlan() (BinaryRepairPlan, error) {
	binary := m.binary
	if strings.TrimSpace(binary.Name) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary name is required")
	}
	if strings.TrimSpace(binary.URL) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary url is required")
	}
	if strings.TrimSpace(binary.Destination) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("binary destination is required")
	}
	if strings.TrimSpace(binary.SHA256) == "" {
		return BinaryRepairPlan{}, fmt.Errorf("sha256 checksum is required for binary repair")
	}
	body, err := os.ReadFile(binary.Destination)
	if os.IsNotExist(err) {
		return BinaryRepairPlan{Actions: []BinaryRepairAction{{Name: binary.Name, URL: binary.URL, Destination: binary.Destination, SHA256: binary.SHA256, Reason: RepairReasonMissing}}}, nil
	}
	if err != nil {
		return BinaryRepairPlan{}, err
	}
	actual, err := SHA256Hex(body)
	if err != nil {
		return BinaryRepairPlan{}, err
	}
	if actual != strings.ToLower(strings.TrimSpace(binary.SHA256)) {
		return BinaryRepairPlan{Actions: []BinaryRepairAction{{Name: binary.Name, URL: binary.URL, Destination: binary.Destination, SHA256: binary.SHA256, Reason: RepairReasonDrifted}}}, nil
	}
	return BinaryRepairPlan{}, nil
}

func (m BinaryAcquisitionModule) DownloadVerified(ctx context.Context, client *http.Client) (DownloadResult, error) {
	binary := m.binary
	return downloadVerifiedBinary(ctx, client, DownloadRequest{
		URL:         binary.URL,
		Destination: binary.Destination,
		SHA256:      binary.SHA256,
		Mode:        0o755,
	})
}

func DownloadVerifiedBinary(ctx context.Context, client *http.Client, req DownloadRequest) (DownloadResult, error) {
	return downloadVerifiedBinary(ctx, client, req)
}

func downloadVerifiedBinary(ctx context.Context, client *http.Client, req DownloadRequest) (DownloadResult, error) {
	if strings.TrimSpace(req.URL) == "" {
		return DownloadResult{}, fmt.Errorf("download url is required")
	}
	if strings.TrimSpace(req.Destination) == "" {
		return DownloadResult{}, fmt.Errorf("download destination is required")
	}
	if strings.TrimSpace(req.SHA256) == "" {
		return DownloadResult{}, fmt.Errorf("sha256 checksum is required")
	}
	if req.Mode == 0 {
		req.Mode = 0o755
	}
	if client == nil {
		client = http.DefaultClient
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<(attempt-2)) * time.Second // 1s, 2s, 4s
			log.Printf("DownloadVerifiedBinary: retry attempt %d/%d for %s after %v (previous error: %v)", attempt, maxAttempts, req.URL, backoff, lastErr)
			select {
			case <-ctx.Done():
				return DownloadResult{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
		if err != nil {
			return DownloadResult{}, err
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = err
			if !isRetryableNetError(err) {
				return DownloadResult{}, err
			}
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("download failed: %s", resp.Status)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return DownloadResult{}, fmt.Errorf("download failed: %s", resp.Status)
		}
		defer resp.Body.Close()
		lr := io.LimitReader(resp.Body, maxReleaseAssetSize+1)
		body, err := io.ReadAll(lr)
		if err != nil {
			lastErr = err
			if !isRetryableNetError(err) {
				return DownloadResult{}, err
			}
			continue
		}
		if len(body) > maxReleaseAssetSize {
			return DownloadResult{}, fmt.Errorf("download %s exceeds maximum size of %d bytes", req.URL, maxReleaseAssetSize)
		}
		actual, err := SHA256Hex(body)
		if err != nil {
			return DownloadResult{}, err
		}
		if err := VerifySHA256Hex(body, req.SHA256); err != nil {
			return DownloadResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(req.Destination), 0o755); err != nil {
			return DownloadResult{}, err
		}
		tmp := req.Destination + ".tmp"
		if err := os.WriteFile(tmp, body, req.Mode); err != nil {
			return DownloadResult{}, err
		}
		if err := os.Chmod(tmp, req.Mode); err != nil {
			_ = os.Remove(tmp)
			return DownloadResult{}, err
		}
		if err := os.Rename(tmp, req.Destination); err != nil {
			_ = os.Remove(tmp)
			return DownloadResult{}, err
		}
		return DownloadResult{URL: req.URL, Destination: req.Destination, SHA256: actual, Bytes: int64(len(body))}, nil
	}
	return DownloadResult{}, fmt.Errorf("download %s failed after %d attempts: %w", req.URL, maxAttempts, lastErr)
}

// isRetryableNetError returns true for network errors that are worth retrying.
func isRetryableNetError(err error) bool {
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
