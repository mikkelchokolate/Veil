package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

const maxPanelUpdateDownloadBytes int64 = 64 * 1024 * 1024

type panelUpdateStager struct {
	root      string
	assetName string
	latest    func(context.Context) (*updateflow.Release, error)
	download  func(context.Context, string) ([]byte, error)
	verify    func(releaseverify.Evidence) error
}

func newPanelUpdateStager(root string) panelUpdateStager {
	catalog := updateflow.NewReleaseCatalog(updateflow.RepoOwner, updateflow.RepoName)
	client := &http.Client{Timeout: 30 * time.Second}
	catalog.HTTPClient = client
	return panelUpdateStager{
		root:      root,
		assetName: updateflow.AssetName(),
		latest: func(ctx context.Context) (*updateflow.Release, error) {
			return catalog.LatestContext(ctx)
		},
		download: func(ctx context.Context, url string) ([]byte, error) {
			return downloadPanelUpdateAsset(ctx, client, url)
		},
		verify: releaseverify.Verify,
	}
}

func (s panelUpdateStager) Stage(ctx context.Context) (string, error) {
	if s.root == "" || s.latest == nil || s.download == nil || s.assetName == "" || s.verify == nil {
		return "", errors.New("panel update stager is not configured")
	}
	release, err := s.latest(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	assets := updateflow.NewReleaseAssetsWithVerifier(release, func(url string) ([]byte, error) {
		return s.download(ctx, url)
	}, s.verify)
	archive, err := assets.DownloadVerifiedArchive()
	if err != nil {
		return "", err
	}
	if archive.Name != s.assetName {
		return "", fmt.Errorf("unexpected update asset %q", archive.Name)
	}
	if err := atomicfile.Write(filepath.Join(s.root, "checksums.txt"), archive.Checksums, 0o600, 0o700); err != nil {
		return "", fmt.Errorf("stage update checksums: %w", err)
	}
	for name, body := range map[string][]byte{
		"checksums.txt.bundle":        archive.ChecksumsBundle,
		"veil.provenance.json":        archive.Provenance,
		"veil.provenance.json.bundle": archive.ProvenanceBundle,
	} {
		if err := atomicfile.Write(filepath.Join(s.root, name), body, 0o600, 0o700); err != nil {
			return "", fmt.Errorf("stage update evidence %s: %w", name, err)
		}
	}
	if err := atomicfile.Write(filepath.Join(s.root, "veil-update.tar.gz"), archive.Body, 0o600, 0o700); err != nil {
		return "", fmt.Errorf("stage update archive: %w", err)
	}
	return release.TagName, nil
}

func downloadPanelUpdateAsset(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "veil")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPanelUpdateDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxPanelUpdateDownloadBytes {
		return nil, errors.New("update asset exceeds size limit")
	}
	return body, nil
}
