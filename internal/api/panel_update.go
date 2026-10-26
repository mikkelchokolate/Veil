package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	versionflow "github.com/mikkelchokolate/Veil/internal/cliflow/version"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

const maxPanelUpdateDownloadBytes int64 = 64 * 1024 * 1024

type panelUpdateManifest struct {
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	Directory string `json:"directory"`
}

// updateRefusedError marks an update target the stager refused on version
// grounds (the running build is already at or newer than the latest release,
// or is a non-release build). The handler maps it to 409 instead of the
// 502 used for download/verification failures (issue #1104).
type updateRefusedError struct{ reason string }

func (e *updateRefusedError) Error() string { return e.reason }

type panelUpdateStager struct {
	root               string
	version            string
	assetName          string
	latest             func(context.Context) (*updateflow.Release, error)
	download           func(context.Context, string) ([]byte, error)
	verify             func(releaseverify.Evidence) error
	resolveCommit      func(context.Context, string) (string, error)
	sourceUpdateCommit func(context.Context, string, string) (string, error)
	// packageManaged reports the owning OS package when the running binary is
	// package-managed; nil skips the check (tests that do not exercise it).
	packageManaged func(string) (string, bool)
}

func newPanelUpdateStager(root string, version string) panelUpdateStager {
	catalog := updateflow.NewReleaseCatalog(updateflow.RepoOwner, updateflow.RepoName)
	client := &http.Client{Timeout: 30 * time.Second}
	catalog.HTTPClient = client
	return panelUpdateStager{
		root:      root,
		version:   version,
		assetName: updateflow.AssetName(),
		latest: func(ctx context.Context) (*updateflow.Release, error) {
			return catalog.LatestContext(ctx)
		},
		download: func(ctx context.Context, url string) ([]byte, error) {
			return downloadPanelUpdateAsset(ctx, client, url)
		},
		verify:             releaseverify.Verify,
		resolveCommit:      catalog.ResolveTagCommit,
		sourceUpdateCommit: catalog.SourceUpdateCommit,
		packageManaged:     updateflow.DetectPackageManagedBinary,
	}
}

func (s panelUpdateStager) Stage(ctx context.Context, force bool) (string, error) {
	if s.root == "" || s.latest == nil || s.download == nil || s.assetName == "" || s.verify == nil || s.resolveCommit == nil {
		return "", errors.New("panel update stager is not configured")
	}
	// A package-managed panel must be upgraded by its package manager: the
	// updater replaces only the executable and leaves the old package's units
	// and install hooks behind (#1424).
	if s.packageManaged != nil {
		if exe, err := os.Executable(); err == nil {
			if owner, managed := s.packageManaged(exe); managed {
				return "", &updateRefusedError{reason: fmt.Sprintf("this Veil binary belongs to %s; update it with the package manager (apt/dnf/apk) instead of the panel updater so the packaged units and install hooks are refreshed. To switch this host to the standalone distribution, remove the package first and install the release archive", owner)}
			}
		}
	}
	release, err := s.latest(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	// A source stamp has no semver order: only a pinned ancestry proof may
	// override refusal before asset downloads without risking a downgrade (#1104).
	var sourceCommit string
	if targetErr := s.checkUpdateTarget(release.TagName, force); targetErr != nil {
		if s.sourceUpdateCommit == nil || versionflow.SourceBuildCommit(s.version) == "" {
			return "", targetErr
		}
		sourceCommit, err = s.sourceUpdateCommit(ctx, s.version, release.TagName)
		if err != nil {
			return "", &updateRefusedError{reason: fmt.Sprintf("%v; cannot verify a forward source-to-release update: %v", targetErr, err)}
		}
		if len(sourceCommit) != 40 {
			return "", &updateRefusedError{reason: "source update proof has an invalid target commit"}
		}
		if _, err := hex.DecodeString(sourceCommit); err != nil {
			return "", &updateRefusedError{reason: "source update proof has an invalid target commit"}
		}
	}
	if sourceCommit == "" {
		sourceCommit, err = s.resolveCommit(ctx, release.TagName)
		if err != nil {
			return "", fmt.Errorf("resolve release tag commit: %w", err)
		}
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

// checkUpdateTarget refuses an update that is not strictly newer than the
// running version unless the operator forced it. A current version that does
// not parse as a release (install-main "main-<sha>" stamps, dev builds) is
// refused here: Stage requires a separate exact-source ancestry proof before
// overriding this refusal, or explicit force (#1104).
func (s panelUpdateStager) checkUpdateTarget(tag string, force bool) error {
	current := strings.TrimSpace(s.version)
	if !versionflow.IsReleaseVersion(current) {
		if force {
			return nil
		}
		return &updateRefusedError{reason: fmt.Sprintf("running build %q is not a release version; refusing to replace it with %s (set force to override)", current, tag)}
	}
	if versionflow.Compare(current, tag) >= 0 && !force {
		return &updateRefusedError{reason: fmt.Sprintf("running version %s is already at or newer than the latest release %s", current, tag)}
	}
	return nil
}

// decodePanelUpdateRequest reads the optional update request body. The body
// is empty (or "{}") for a normal update; {"force": true} overrides the
// stager's not-strictly-newer refusal — the operator-side escape hatch for
// intentionally replacing a main/prerelease build (issue #1104).
func decodePanelUpdateRequest(r *http.Request) (bool, error) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONMediaType(ct) {
		return false, errUnsupportedJSONMediaType
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return false, errJSONBodyTooLarge
		}
		return false, err
	}
	if trimmed := strings.TrimSpace(string(body)); trimmed == "" || trimmed == "{}" {
		return false, nil
	}
	var request struct {
		Force bool `json:"force"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return false, errors.New("invalid JSON update request; expected {} or {\"force\":true}")
	}
	return request.Force, nil
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
