package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestUpdateReleaseAssetsDownloadsVerifiedArchive(t *testing.T) {
	assetName := updateAssetName()
	archive := []byte("archive-body")
	hash := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), assetName))
	downloaded := []string{}

	assets := NewUpdateReleaseAssets(&githubRelease{TagName: "v1.2.4", Assets: []githubAsset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/archive"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}}, func(url string) ([]byte, error) {
		downloaded = append(downloaded, url)
		switch url {
		case "https://example.com/archive":
			return archive, nil
		case "https://example.com/checksums":
			return checksums, nil
		default:
			t.Fatalf("unexpected download URL %s", url)
			return nil, nil
		}
	})

	got, err := assets.DownloadVerifiedArchive()
	if err != nil {
		t.Fatalf("DownloadVerifiedArchive: %v", err)
	}
	if got.Name != assetName || string(got.Body) != string(archive) {
		t.Fatalf("archive = %+v", got)
	}
	if fmt.Sprint(downloaded) != "[https://example.com/archive https://example.com/checksums]" {
		t.Fatalf("downloaded = %v", downloaded)
	}
}
