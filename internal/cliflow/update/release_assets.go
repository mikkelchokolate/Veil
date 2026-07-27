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

	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

type Archive struct {
	Name             string
	Body             []byte
	Checksums        []byte
	ChecksumsBundle  []byte
	Provenance       []byte
	ProvenanceBundle []byte
}

type ReleaseAssets struct {
	release    *Release
	downloader func(string) ([]byte, error)
	assetName  string
	verifier   func(releaseverify.Evidence) error
}

func NewReleaseAssets(release *Release, downloader func(string) ([]byte, error)) ReleaseAssets {
	return ReleaseAssets{
		release:    release,
		downloader: downloader,
		assetName:  AssetName(),
		verifier:   releaseverify.Verify,
	}
}

func NewReleaseAssetsWithVerifier(release *Release, downloader func(string) ([]byte, error), verifier func(releaseverify.Evidence) error) ReleaseAssets {
	assets := NewReleaseAssets(release, downloader)
	assets.verifier = verifier
	return assets
}

func (a ReleaseAssets) DownloadVerifiedArchive() (Archive, error) {
	if a.release == nil {
		return Archive{}, fmt.Errorf("release metadata is missing")
	}
	const (
		checksumsName        = "checksums.txt"
		checksumsBundleName  = "checksums.txt.bundle"
		provenanceName       = "veil.provenance.json"
		provenanceBundleName = "veil.provenance.json.bundle"
	)
	assetURL := FindAssetURL(a.release.Assets, a.assetName)
	checksumsURL := FindAssetURL(a.release.Assets, checksumsName)
	checksumsBundleURL := FindAssetURL(a.release.Assets, checksumsBundleName)
	provenanceURL := FindAssetURL(a.release.Assets, provenanceName)
	provenanceBundleURL := FindAssetURL(a.release.Assets, provenanceBundleName)
	if assetURL == "" {
		return Archive{}, fmt.Errorf("release %s has no asset %s", a.release.TagName, a.assetName)
	}
	if checksumsURL == "" {
		return Archive{}, fmt.Errorf("release %s has no checksums asset", a.release.TagName)
	}
	if checksumsBundleURL == "" || provenanceURL == "" || provenanceBundleURL == "" {
		return Archive{}, fmt.Errorf("release %s is missing signed provenance assets", a.release.TagName)
	}

	archive, err := a.downloader(assetURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download %s: %w", a.assetName, err)
	}
	checksumsBody, err := a.downloader(checksumsURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download checksums: %w", err)
	}
	checksumsBundle, err := a.downloader(checksumsBundleURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download checksum bundle: %w", err)
	}
	provenance, err := a.downloader(provenanceURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download provenance: %w", err)
	}
	provenanceBundle, err := a.downloader(provenanceBundleURL)
	if err != nil {
		return Archive{}, fmt.Errorf("download provenance bundle: %w", err)
	}
	if a.verifier == nil {
		return Archive{}, fmt.Errorf("release provenance verifier is not configured")
	}
	if err := a.verifier(releaseverify.Evidence{
		Repository:       RepoOwner + "/" + RepoName,
		WorkflowPath:     ".github/workflows/release.yml",
		ReleaseTag:       a.release.TagName,
		ArchiveName:      a.assetName,
		Archive:          archive,
		ChecksumsName:    checksumsName,
		Checksums:        checksumsBody,
		ChecksumsBundle:  checksumsBundle,
		Provenance:       provenance,
		ProvenanceBundle: provenanceBundle,
	}); err != nil {
		return Archive{}, fmt.Errorf("release signature/provenance verification failed: %w", err)
	}
	// The checksum file becomes trusted only after its signature and provenance
	// have passed independent verification above.
	if err := VerifyAssetChecksum(archive, a.assetName, string(checksumsBody)); err != nil {
		return Archive{}, fmt.Errorf("checksum verification failed: %w", err)
	}
	return Archive{
		Name: a.assetName, Body: archive, Checksums: checksumsBody,
		ChecksumsBundle: checksumsBundle, Provenance: provenance, ProvenanceBundle: provenanceBundle,
	}, nil
}

var assetNameGOARCH = func() string { return runtime.GOARCH }

// AssetName returns the expected release asset name for the current platform.
func AssetName() string {
	osName := runtime.GOOS
	arch := assetNameGOARCH()
	switch arch {
	case "amd64", "x86_64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	}
	return fmt.Sprintf("veil_%s_%s.tar.gz", osName, arch)
}

// FindAssetURL returns the download URL for the named asset.
func FindAssetURL(assets []Asset, name string) string {
	for _, asset := range assets {
		if asset.Name == name {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

// DownloadAsset downloads a URL and returns the body bytes.
func DownloadAsset(rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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
	const maxSize = 50 * 1024 * 1024
	limited := io.LimitReader(resp.Body, maxSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxSize {
		return nil, fmt.Errorf("release asset exceeds %d bytes", maxSize)
	}
	return body, nil
}

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

func ExtractChecksumForFile(checksumsText, filename string) string {
	for _, line := range strings.Split(checksumsText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == filename && i > 0 {
				return fields[i-1]
			}
		}
		if len(fields) >= 2 && fields[len(fields)-1] == filename {
			return fields[0]
		}
	}
	return ""
}
