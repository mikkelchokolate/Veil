package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

const maxPanelUpdateDownloadBytes int64 = 64 * 1024 * 1024

type panelUpdateManifest struct {
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	Directory string `json:"directory"`
}

type panelUpdateStager struct {
	root          string
	assetName     string
	latest        func(context.Context) (*updateflow.Release, error)
	download      func(context.Context, string) ([]byte, error)
	verify        func(releaseverify.Evidence) error
	resolveCommit func(context.Context, string) (string, error)
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
		resolveCommit: func(ctx context.Context, tag string) (string, error) {
			return resolveGitHubTagCommit(ctx, client, updateflow.RepoOwner, updateflow.RepoName, tag)
		},
	}
}

func (s panelUpdateStager) Stage(ctx context.Context) (string, error) {
	if s.root == "" || s.latest == nil || s.download == nil || s.assetName == "" || s.verify == nil || s.resolveCommit == nil {
		return "", errors.New("panel update stager is not configured")
	}
	release, err := s.latest(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	sourceCommit, err := s.resolveCommit(ctx, release.TagName)
	if err != nil {
		return "", fmt.Errorf("resolve release tag commit: %w", err)
	}
	assets := updateflow.NewReleaseAssetsWithVerifier(release, func(url string) ([]byte, error) {
		return s.download(ctx, url)
	}, func(evidence releaseverify.Evidence) error {
		evidence.SourceCommit = sourceCommit
		return s.verify(evidence)
	})
	archive, err := assets.DownloadVerifiedArchive()
	if err != nil {
		return "", err
	}
	if archive.Name != s.assetName {
		return "", fmt.Errorf("unexpected update asset %q", archive.Name)
	}
	if release.TagName == "" || strings.Contains(release.TagName, "/") || filepath.Base(release.TagName) != release.TagName {
		return "", errors.New("invalid update version")
	}
	digestBytes := sha256.Sum256(archive.Body)
	digest := hex.EncodeToString(digestBytes[:])
	relativeDirectory := filepath.Join("versions", release.TagName, digest)
	stageRoot := filepath.Join(s.root, relativeDirectory)
	if err := atomicfile.Write(filepath.Join(stageRoot, "checksums.txt"), archive.Checksums, 0o600, 0o700); err != nil {
		return "", fmt.Errorf("stage update checksums: %w", err)
	}
	for name, body := range map[string][]byte{
		"checksums.txt.bundle":        archive.ChecksumsBundle,
		"veil.provenance.json":        archive.Provenance,
		"veil.provenance.json.bundle": archive.ProvenanceBundle,
	} {
		if err := atomicfile.Write(filepath.Join(stageRoot, name), body, 0o600, 0o700); err != nil {
			return "", fmt.Errorf("stage update evidence %s: %w", name, err)
		}
	}
	if err := atomicfile.Write(filepath.Join(stageRoot, "veil-update.tar.gz"), archive.Body, 0o600, 0o700); err != nil {
		return "", fmt.Errorf("stage update archive: %w", err)
	}
	manifest, err := json.Marshal(panelUpdateManifest{Version: release.TagName, Digest: digest, Directory: filepath.ToSlash(relativeDirectory)})
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(filepath.Join(s.root, "update-manifest.json"), manifest, 0o600, 0o700); err != nil {
		return "", fmt.Errorf("publish update manifest: %w", err)
	}
	return release.TagName, nil
}

func resolveGitHubTagCommit(ctx context.Context, client *http.Client, owner, repository, tag string) (string, error) {
	if client == nil || owner == "" || repository == "" || tag == "" {
		return "", errors.New("GitHub tag resolver is not configured")
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/tags/%s", url.PathEscape(owner), url.PathEscape(repository), url.PathEscape(tag))
	for depth := 0; depth < 3; depth++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("User-Agent", "veil")
		response, err := client.Do(request)
		if err != nil {
			return "", err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		closeErr := response.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", fmt.Errorf("GitHub tag lookup status %d", response.StatusCode)
		}
		var result struct {
			Object struct{ SHA, Type string } `json:"object"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", err
		}
		sha := strings.ToLower(strings.TrimSpace(result.Object.SHA))
		decoded, err := hex.DecodeString(sha)
		if err != nil || len(decoded) != 20 {
			return "", errors.New("GitHub tag resolved to an invalid commit identity")
		}
		if result.Object.Type == "commit" {
			return sha, nil
		}
		if result.Object.Type != "tag" {
			return "", fmt.Errorf("GitHub tag resolved to unsupported object type %q", result.Object.Type)
		}
		endpoint = fmt.Sprintf("https://api.github.com/repos/%s/%s/git/tags/%s", url.PathEscape(owner), url.PathEscape(repository), sha)
	}
	return "", errors.New("GitHub annotated tag chain exceeds maximum depth")
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
