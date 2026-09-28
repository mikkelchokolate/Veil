package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

func TestPanelUpdateStagerPersistsVerifiedArchiveAndChecksums(t *testing.T) {
	root := t.TempDir()
	archive := []byte("release-archive")
	hash := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), updateflow.AssetName()))
	stager := panelUpdateStager{
		root:      root,
		version:   "v0.5.0",
		assetName: updateflow.AssetName(),
		latest: func(context.Context) (*updateflow.Release, error) {
			return &updateflow.Release{TagName: "v0.6.0", Assets: []updateflow.Asset{
				{Name: updateflow.AssetName(), BrowserDownloadURL: "https://example.invalid/archive"},
				{Name: "checksums.txt", BrowserDownloadURL: "https://example.invalid/checksums"},
				{Name: "checksums.txt.bundle", BrowserDownloadURL: "https://example.invalid/checksums.bundle"},
				{Name: "veil.provenance.json", BrowserDownloadURL: "https://example.invalid/provenance"},
				{Name: "veil.provenance.json.bundle", BrowserDownloadURL: "https://example.invalid/provenance.bundle"},
			}}, nil
		},
		download: func(_ context.Context, url string) ([]byte, error) {
			switch url {
			case "https://example.invalid/archive":
				return archive, nil
			case "https://example.invalid/checksums":
				return checksums, nil
			default:
				return []byte("signed-evidence"), nil
			}
		},
		resolveCommit: func(context.Context, string) (string, error) { return strings.Repeat("a", 40), nil },
		verify: func(evidence releaseverify.Evidence) error {
			if evidence.SourceCommit != strings.Repeat("a", 40) {
				return fmt.Errorf("source commit = %q", evidence.SourceCommit)
			}
			return nil
		},
	}

	version, err := stager.Stage(context.Background(), false)
	if err != nil {
		t.Fatalf("stage update: %v", err)
	}
	if version != "v0.6.0" {
		t.Fatalf("version=%q", version)
	}
	manifestBody, err := os.ReadFile(filepath.Join(root, "update-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest panelUpdateManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != version || manifest.Digest != hex.EncodeToString(hash[:]) {
		t.Fatalf("manifest=%+v", manifest)
	}
	stageRoot := filepath.Join(root, filepath.FromSlash(manifest.Directory))
	gotArchive, err := os.ReadFile(filepath.Join(stageRoot, "veil-update.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gotChecksums, err := os.ReadFile(filepath.Join(stageRoot, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotArchive) != string(archive) || string(gotChecksums) != string(checksums) {
		t.Fatalf("staged archive=%q checksums=%q", gotArchive, gotChecksums)
	}
}

// TestPanelUpdateStagerRefusesNonNewerOrNonReleaseTarget is the #1104
// regression: installing the "latest" tag over a build that is already at or
// newer than it — or over a non-release install-main/dev build that never
// orders against tags — silently downgrades, and a schema-skewed downgrade
// leaves the panel unable to open state.json after the restart. The stager
// must refuse BEFORE downloading unless the operator forces it.
func TestPanelUpdateStagerRefusesNonNewerOrNonReleaseTarget(t *testing.T) {
	latestTag := "v0.7.2"
	newStager := func(version string, downloadCalled *int32) panelUpdateStager {
		return panelUpdateStager{
			root:      t.TempDir(),
			version:   version,
			assetName: updateflow.AssetName(),
			latest: func(context.Context) (*updateflow.Release, error) {
				return &updateflow.Release{TagName: latestTag, Assets: []updateflow.Asset{
					{Name: updateflow.AssetName(), BrowserDownloadURL: "https://example.invalid/archive"},
				}}, nil
			},
			download: func(context.Context, string) ([]byte, error) {
				atomic.AddInt32(downloadCalled, 1)
				return []byte("x"), nil
			},
			resolveCommit: func(context.Context, string) (string, error) { return strings.Repeat("a", 40), nil },
			verify:        func(releaseverify.Evidence) error { return nil },
		}
	}

	for _, running := range []string{"v0.8.0-rc1", "v0.7.2", "v0.9.0", "main-abcdef1", "dev", ""} {
		var downloads int32
		stager := newStager(running, &downloads)
		_, err := stager.Stage(context.Background(), false)
		var refused *updateRefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("running=%q: Stage should refuse target %s, got err=%v", running, latestTag, err)
		}
		if got := atomic.LoadInt32(&downloads); got != 0 {
			t.Fatalf("running=%q: refusal happened after %d downloads — must refuse before staging", running, got)
		}
	}
}

// TestPanelUpdateStagerRefusalSurfacesAsConflict pins the handler mapping: a
// refused target is a client-level 409, not the 502 used for download or
// verification failures (issue #1104).
func TestPanelUpdateStagerRefusalSurfacesAsConflict(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	state.privileged = &recordingPrivilegedClient{}
	state.updateStager = func(context.Context, bool) (string, error) {
		return "", &updateRefusedError{reason: "running version v0.9.0 is already at or newer than the latest release v0.7.2"}
	}
	routes := PanelRoutes{Info: ServerInfo{Version: "0.9.0"}, State: state}
	response := httptest.NewRecorder()
	routes.handleUpdateVersion(response, httptest.NewRequest(http.MethodPost, "/api/version/update", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("refused update status=%d body=%s, want 409", response.Code, response.Body.String())
	}
}

// TestDecodePanelUpdateRequestForce pins the operator escape hatch: an empty
// body is a normal update, {"force":true} overrides the not-newer refusal.
func TestDecodePanelUpdateRequestForce(t *testing.T) {
	for _, tc := range []struct {
		body  string
		want  bool
		valid bool
	}{
		{"", false, true},
		{"{}", false, true},
		{`{"force":true}`, true, true},
		{`{"force":false}`, false, true},
		{`{"unknown":1}`, false, false},
		{"not json", false, false},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/version/update", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		got, err := decodePanelUpdateRequest(req)
		if tc.valid != (err == nil) {
			t.Fatalf("body=%q: err=%v, want valid=%v", tc.body, err, tc.valid)
		}
		if tc.valid && got != tc.want {
			t.Fatalf("body=%q: force=%v, want %v", tc.body, got, tc.want)
		}
	}
}
