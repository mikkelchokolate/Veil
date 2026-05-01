package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type BuildHint struct {
	BinaryPath string
	Commands   []string
}

type DownloadRequest struct {
	URL         string
	Destination string
	SHA256      string
	Mode        os.FileMode
}

type DownloadResult struct {
	URL         string
	Destination string
	SHA256      string
	Bytes       int64
}

func Hysteria2ReleaseAssetURL(version, goos, arch string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("version is required")
	}
	if goos != "linux" {
		return "", fmt.Errorf("unsupported os %q", goos)
	}
	mappedArch, err := hysteriaArch(arch)
	if err != nil {
		return "", err
	}
	tag := "app/" + version
	asset := fmt.Sprintf("hysteria-%s-%s", goos, mappedArch)
	return fmt.Sprintf("https://github.com/apernet/hysteria/releases/download/%s/%s", url.PathEscape(tag), asset), nil
}

func hysteriaArch(arch string) (string, error) {
	switch arch {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported arch %q", arch)
	}
}

func SHA256Hex(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func VerifySHA256Hex(data []byte, expected string) error {
	actual, err := SHA256Hex(data)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func DownloadVerifiedBinary(ctx context.Context, client *http.Client, req DownloadRequest) (DownloadResult, error) {
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return DownloadResult{}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DownloadResult{}, fmt.Errorf("download failed: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DownloadResult{}, err
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

func CaddyNaiveBuildHint(binaryPath string) BuildHint {
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/caddy"
	}
	return BuildHint{
		BinaryPath: binaryPath,
		Commands: []string{
			"go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest",
			"xcaddy build --with github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@naive",
			"install -m 0755 ./caddy " + binaryPath,
		},
	}
}
