package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
)

type UpdateArchive struct {
	Name string
	Body []byte
}

type UpdateReleaseAssets struct {
	release    *githubRelease
	downloader func(string) ([]byte, error)
	assetName  string
}

func NewUpdateReleaseAssets(release *githubRelease, downloader func(string) ([]byte, error)) UpdateReleaseAssets {
	return UpdateReleaseAssets{
		release:    release,
		downloader: downloader,
		assetName:  updateAssetName(),
	}
}

func (a UpdateReleaseAssets) DownloadVerifiedArchive() (UpdateArchive, error) {
	checksumsName := "checksums.txt"
	assetURL := findAssetURL(a.release.Assets, a.assetName)
	checksumsURL := findAssetURL(a.release.Assets, checksumsName)
	if assetURL == "" {
		return UpdateArchive{}, fmt.Errorf("release %s has no asset %s", a.release.TagName, a.assetName)
	}
	if checksumsURL == "" {
		return UpdateArchive{}, fmt.Errorf("release %s has no checksums asset", a.release.TagName)
	}
	archive, err := a.downloader(assetURL)
	if err != nil {
		return UpdateArchive{}, fmt.Errorf("download %s: %w", a.assetName, err)
	}
	checksumsBody, err := a.downloader(checksumsURL)
	if err != nil {
		return UpdateArchive{}, fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyAssetChecksum(archive, a.assetName, string(checksumsBody)); err != nil {
		return UpdateArchive{}, fmt.Errorf("checksum verification failed: %w", err)
	}
	return UpdateArchive{Name: a.assetName, Body: archive}, nil
}

// updateAssetName returns the expected release asset name for the current platform.
func updateAssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "x86_64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	}
	return fmt.Sprintf("veil_%s_%s.tar.gz", os, arch)
}

// findAssetURL returns the download URL for the named asset.
func findAssetURL(assets []githubAsset, name string) string {
	for _, a := range assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// downloadAsset downloads a URL and returns the body bytes.
func downloadAsset(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	const maxSize = 50 * 1024 * 1024 // 50 MB
	return io.ReadAll(io.LimitReader(resp.Body, maxSize))
}

// verifyAssetChecksum verifies that archive bytes match the expected SHA256 in checksumsText.
func verifyAssetChecksum(archive []byte, assetName, checksumsText string) error {
	expected := extractChecksumForFile(checksumsText, assetName)
	if expected == "" {
		return fmt.Errorf("no checksum found for %s", assetName)
	}
	actual := sha256.Sum256(archive)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(actualHex, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actualHex)
	}
	return nil
}

// extractChecksumForFile finds the SHA256 hex for a filename in checksums.txt output.
func extractChecksumForFile(checksumsText, filename string) string {
	for _, line := range strings.Split(checksumsText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == filename && i > 0 {
				return fields[i-1]
			}
		}
		// Also try "filename" at end (sha256sum format: hash  filename)
		if len(fields) >= 2 && fields[len(fields)-1] == filename {
			return fields[0]
		}
	}
	return ""
}

func downloadVerifiedUpdateAsset(release *githubRelease) (string, []byte, error) {
	archive, err := NewUpdateReleaseAssets(release, updateAssetDownloader).DownloadVerifiedArchive()
	if err != nil {
		return "", nil, err
	}
	return archive.Name, archive.Body, nil
}
