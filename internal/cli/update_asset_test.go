package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
)

func TestDownloadVerifiedUpdateAssetDownloadsArchiveAndChecksChecksum(t *testing.T) {
	assetName := updateflow.AssetName()
	archive := []byte("archive-body")
	hash := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), assetName))

	oldDownloader := updateAssetDownloader
	updateAssetDownloader = func(url string) ([]byte, error) {
		switch url {
		case "https://example.com/archive":
			return archive, nil
		case "https://example.com/checksums":
			return checksums, nil
		default:
			t.Fatalf("unexpected download URL %s", url)
			return nil, nil
		}
	}
	t.Cleanup(func() { updateAssetDownloader = oldDownloader })

	gotName, gotArchive, err := downloadVerifiedUpdateAsset(&updateflow.Release{TagName: "v1.2.4", Assets: []updateflow.Asset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/archive"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}})
	if err != nil {
		t.Fatalf("downloadVerifiedUpdateAsset: %v", err)
	}
	if gotName != assetName || string(gotArchive) != string(archive) {
		t.Fatalf("unexpected asset %q %q", gotName, string(gotArchive))
	}
}
