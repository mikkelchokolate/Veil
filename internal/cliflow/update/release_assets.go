package update

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

type Archive struct {
	Name      string
	Body      []byte
	Checksums []byte
}

type ReleaseAssets struct {
	release    *Release
	downloader func(string) ([]byte, error)
	assetName  string
}

func NewReleaseAssets(release *Release, downloader func(string) ([]byte, error)) ReleaseAssets {
	return ReleaseAssets{
		release:    release,
		downloader: downloader,
		assetName:  AssetName(),
	}
}

func (a ReleaseAssets) DownloadVerifiedArchive() (Archive, error) {
	checksumsName := "checksums.txt"
	assetURL := FindAssetURL(a.release.Assets, a.assetName)
	checksumsURL := FindAssetURL(a.release.Assets, checksumsName)
	if assetURL == "" {
		return Archive{}, fmt.Errorf("release %s has no asset %s", a.release.TagName, a.assetName)
	}
	if checksumsURL == "" {
		return Archive{}, fmt.Errorf("release %s has no checksums asset", a.release.TagName)
	}
	archive, err := a.downloader(assetURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download %s: %w", a.assetName, err)
	}
	checksumsBody, err := a.downloader(checksumsURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download checksums: %w", err)
	}
	if err := VerifyAssetChecksum(archive, a.assetName, string(checksumsBody)); err != nil {
		return Archive{}, fmt.Errorf("checksum verification failed: %w", err)
	}
	return Archive{Name: a.assetName, Body: archive, Checksums: checksumsBody}, nil
}

var assetNameGOARCH = func() string { return runtime.GOARCH }

// AssetName returns the expected release asset name for the current platform.
func AssetName() string {
	os := runtime.GOOS
	arch := assetNameGOARCH()
	switch arch {
	case "amd64", "x86_64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	}
	return fmt.Sprintf("veil_%s_%s.tar.gz", os, arch)
}

// FindAssetURL returns the download URL for the named asset.
func FindAssetURL(assets []Asset, name string) string {
	for _, a := range assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// DownloadAsset downloads a URL and returns the body bytes.
func DownloadAsset(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := HTTPClient.Do(req)
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

// VerifyAssetChecksum verifies that archive bytes match the expected SHA256 in checksumsText.
func VerifyAssetChecksum(archive []byte, assetName, checksumsText string) error {
	expected := ExtractChecksumForFile(checksumsText, assetName)
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

// ExtractChecksumForFile finds the SHA256 hex for a filename in checksums.txt output.
func ExtractChecksumForFile(checksumsText, filename string) string {
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
