package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
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
			}}, nil
		},
		download: func(_ context.Context, url string) ([]byte, error) {
			switch url {
			case "https://example.invalid/archive":
				return archive, nil
			case "https://example.invalid/checksums":
				return checksums, nil
			default:
				return nil, fmt.Errorf("unexpected URL %s", url)
			}
		},
	}

	version, err := stager.Stage(context.Background())
	if err != nil {
		t.Fatalf("stage update: %v", err)
	}
	if version != "v0.6.0" {
		t.Fatalf("version=%q", version)
	}
	gotArchive, err := os.ReadFile(filepath.Join(root, "veil-update.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gotChecksums, err := os.ReadFile(filepath.Join(root, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotArchive) != string(archive) || string(gotChecksums) != string(checksums) {
		t.Fatalf("staged archive=%q checksums=%q", gotArchive, gotChecksums)
	}
}
