package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	version, err := stager.Stage(context.Background())
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
